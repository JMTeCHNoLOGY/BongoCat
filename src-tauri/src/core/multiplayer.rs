use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    collections::{HashMap, HashSet},
    sync::{
        Arc, Mutex, RwLock,
        atomic::{AtomicU64, Ordering},
    },
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tauri::{AppHandle, Emitter, State, command};
use tokio::sync::{mpsc, oneshot};
use tokio::time::{Instant, interval, sleep, timeout};
use tokio_tungstenite::{connect_async, tungstenite::Message as WebSocketMessage};
use url::Url;

pub const MESSAGE_EVENT: &str = "multiplayer-message";
pub const STATUS_EVENT: &str = "multiplayer-status";

const PROTOCOL_VERSION: &str = "v1";
const DEFAULT_MAX_EVENTS_PER_SECOND: u32 = 512;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RoomPolicy {
    pub stream_mode: String,
    pub max_players: usize,
    pub max_events_per_second: u32,
    pub continuous_hz: u32,
    pub max_message_bytes: u64,
    pub snapshot_interval_ms: u64,
}

impl Default for RoomPolicy {
    fn default() -> Self {
        Self {
            stream_mode: "raw".into(),
            max_players: 8,
            max_events_per_second: DEFAULT_MAX_EVENTS_PER_SECOND,
            continuous_hz: 20,
            max_message_bytes: 16_384,
            snapshot_interval_ms: 1_000,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PlayerProfile {
    pub player_id: String,
    pub name: String,
    pub skin_id: String,
    pub mode: String,
    pub order: u64,
    pub online: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct JoinProfile {
    pub name: String,
    pub skin_id: String,
    pub mode: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RoomJoined {
    pub room_code: String,
    #[serde(rename = "self")]
    pub self_player: PlayerProfile,
    pub players: Vec<PlayerProfile>,
    pub resume_token: String,
    pub policy: RoomPolicy,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DisplayBounds {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InputEvent {
    pub sequence: u64,
    pub client_time_ms: u64,
    pub kind: String,
    pub value: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub bounds: Option<DisplayBounds>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CursorState {
    pub x: f64,
    pub y: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub bounds: Option<DisplayBounds>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct InputSnapshot {
    pub sequence: u64,
    pub client_time_ms: u64,
    pub pressed_keys: Vec<String>,
    pub mouse_buttons: Vec<String>,
    pub cursor: Option<CursorState>,
    pub gamepad: HashMap<String, f64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProtocolMessage {
    #[serde(rename = "v")]
    pub version: String,
    #[serde(rename = "type")]
    pub message_type: String,
    #[serde(rename = "requestId", skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
    #[serde(default)]
    pub payload: Value,
}

impl ProtocolMessage {
    fn new(message_type: &str, payload: Value) -> Self {
        Self {
            version: PROTOCOL_VERSION.into(),
            message_type: message_type.into(),
            request_id: Some(format!("desktop-{}", now_ms())),
            payload,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProtocolError {
    pub code: String,
    pub message: String,
}

impl ProtocolError {
    fn connection(message: impl Into<String>) -> Self {
        Self {
            code: "CONNECTION_FAILED".into(),
            message: message.into(),
        }
    }
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct MultiplayerStatus {
    pub state: String,
    pub room: Option<RoomJoined>,
    pub error: Option<ProtocolError>,
}

#[derive(Default)]
struct InputState {
    pressed_keys: HashSet<String>,
    mouse_buttons: HashSet<String>,
    cursor: Option<CursorState>,
    gamepad: HashMap<String, f64>,
    continuous_dirty: bool,
}

struct LocalRate {
    window: Instant,
    count: u32,
}

impl Default for LocalRate {
    fn default() -> Self {
        Self {
            window: Instant::now(),
            count: 0,
        }
    }
}

impl LocalRate {
    fn allow(&mut self, limit: u32) -> bool {
        let now = Instant::now();
        if now.duration_since(self.window) >= Duration::from_secs(1) {
            self.window = now;
            self.count = 0;
        }
        self.count += 1;
        self.count <= limit
    }
}

enum SessionCommand {
    Send(ProtocolMessage),
    Leave,
}

enum InitialAction {
    Create(JoinProfile),
    Join(String, JoinProfile),
}

enum ConnectedExit {
    Disconnected(String),
    Left,
}

struct Inner {
    generation: AtomicU64,
    sequence: AtomicU64,
    sender: Mutex<Option<(u64, mpsc::Sender<SessionCommand>)>>,
    room: RwLock<Option<RoomJoined>>,
    policy: RwLock<RoomPolicy>,
    input: Mutex<InputState>,
    rate: Mutex<LocalRate>,
    bounds: RwLock<Option<DisplayBounds>>,
}

#[derive(Clone)]
pub struct MultiplayerManager {
    inner: Arc<Inner>,
}

impl Default for MultiplayerManager {
    fn default() -> Self {
        Self {
            inner: Arc::new(Inner {
                generation: AtomicU64::new(0),
                sequence: AtomicU64::new(0),
                sender: Mutex::new(None),
                room: RwLock::new(None),
                policy: RwLock::new(RoomPolicy::default()),
                input: Mutex::new(InputState::default()),
                rate: Mutex::new(LocalRate::default()),
                bounds: RwLock::new(None),
            }),
        }
    }
}

impl MultiplayerManager {
    async fn start(
        &self,
        app: AppHandle,
        endpoint: String,
        action: InitialAction,
    ) -> Result<RoomJoined, ProtocolError> {
        validate_endpoint(&endpoint)?;
        self.stop().await;
        *self.inner.input.lock().unwrap() = InputState::default();

        let generation = self.inner.generation.fetch_add(1, Ordering::SeqCst) + 1;
        let (sender, receiver) = mpsc::channel(256);
        let (ready_sender, ready_receiver) = oneshot::channel();
        *self.inner.sender.lock().unwrap() = Some((generation, sender));

        let manager = self.clone();
        tauri::async_runtime::spawn(async move {
            manager
                .run_session(app, endpoint, action, generation, receiver, ready_sender)
                .await;
        });

        ready_receiver
            .await
            .unwrap_or_else(|_| Err(ProtocolError::connection("connection task stopped")))
    }

    async fn stop(&self) {
        self.inner.generation.fetch_add(1, Ordering::SeqCst);
        let sender = self.inner.sender.lock().unwrap().take();
        if let Some((_, sender)) = sender {
            let _ = sender.send(SessionCommand::Leave).await;
        }
        *self.inner.room.write().unwrap() = None;
        *self.inner.input.lock().unwrap() = InputState::default();
    }

    async fn run_session(
        &self,
        app: AppHandle,
        endpoint: String,
        action: InitialAction,
        generation: u64,
        mut receiver: mpsc::Receiver<SessionCommand>,
        ready_sender: oneshot::Sender<Result<RoomJoined, ProtocolError>>,
    ) {
        self.emit_status(&app, "connecting", None);
        let initial = self.connect_initial(&endpoint, action).await;
        let (mut stream, mut joined, policy) = match initial {
            Ok(value) => value,
            Err(error) => {
                let _ = ready_sender.send(Err(error.clone()));
                if generation == self.inner.generation.load(Ordering::SeqCst) {
                    self.emit_status(&app, "disconnected", Some(error));
                    self.clear(generation);
                }
                return;
            }
        };

        if generation != self.inner.generation.load(Ordering::SeqCst) {
            let _ = stream.close(None).await;
            let _ = ready_sender.send(Err(ProtocolError::connection("connection cancelled")));
            return;
        }

        *self.inner.policy.write().unwrap() = policy;
        *self.inner.room.write().unwrap() = Some(joined.clone());
        let _ = ready_sender.send(Ok(joined.clone()));
        self.emit_message(&app, "room_joined", &joined);
        self.emit_status(&app, "connected", None);

        loop {
            match self.connected_loop(&app, &mut stream, &mut receiver).await {
                ConnectedExit::Left => break,
                ConnectedExit::Disconnected(message) => {
                    self.emit_status(
                        &app,
                        "reconnecting",
                        Some(ProtocolError::connection(message)),
                    );
                }
            }

            let deadline = Instant::now() + Duration::from_secs(15);
            let mut delay = Duration::from_millis(500);
            let mut resumed = None;

            while Instant::now() < deadline {
                if generation != self.inner.generation.load(Ordering::SeqCst) {
                    self.clear(generation);
                    return;
                }

                let resume = timeout(
                    Duration::from_secs(5),
                    self.connect_resume(&endpoint, &joined),
                )
                .await;

                if let Ok(Ok(value)) = resume {
                    resumed = Some(value);
                    break;
                }

                tokio::select! {
                    command = receiver.recv() => {
                        if matches!(command, Some(SessionCommand::Leave) | None) {
                            self.clear(generation);
                            return;
                        }
                    }
                    _ = sleep(delay) => {}
                }
                delay = (delay * 2).min(Duration::from_secs(3));
            }

            let Some((next_stream, next_joined, policy)) = resumed else {
                self.emit_status(
                    &app,
                    "disconnected",
                    Some(ProtocolError::connection("resume grace period expired")),
                );
                break;
            };

            if generation != self.inner.generation.load(Ordering::SeqCst) {
                return;
            }

            stream = next_stream;
            joined = next_joined;
            *self.inner.policy.write().unwrap() = policy;
            *self.inner.room.write().unwrap() = Some(joined.clone());
            self.emit_message(&app, "room_joined", &joined);
            self.emit_status(&app, "connected", None);
        }

        if generation == self.inner.generation.load(Ordering::SeqCst) {
            self.clear(generation);
            self.emit_status(&app, "disconnected", None);
        }
    }

    async fn connect_initial(
        &self,
        endpoint: &str,
        action: InitialAction,
    ) -> Result<(WebSocketStream, RoomJoined, RoomPolicy), ProtocolError> {
        let request = match action {
            InitialAction::Create(profile) => {
                ProtocolMessage::new("create_room", json!({ "profile": profile }))
            }
            InitialAction::Join(room_code, profile) => ProtocolMessage::new(
                "join_room",
                json!({ "roomCode": room_code, "profile": profile }),
            ),
        };
        connect_and_request(endpoint, request).await
    }

    async fn connect_resume(
        &self,
        endpoint: &str,
        joined: &RoomJoined,
    ) -> Result<(WebSocketStream, RoomJoined, RoomPolicy), ProtocolError> {
        let request = ProtocolMessage::new(
            "resume_room",
            json!({
                "roomCode": joined.room_code,
                "playerId": joined.self_player.player_id,
                "resumeToken": joined.resume_token,
            }),
        );
        connect_and_request(endpoint, request).await
    }

    async fn connected_loop(
        &self,
        app: &AppHandle,
        stream: &mut WebSocketStream,
        receiver: &mut mpsc::Receiver<SessionCommand>,
    ) -> ConnectedExit {
        let policy = self.inner.policy.read().unwrap().clone();
        let snapshot_ms = policy.snapshot_interval_ms.max(250);
        let continuous_ms = (1_000 / policy.continuous_hz.max(1)) as u64;
        let mut snapshot_tick = interval(Duration::from_millis(snapshot_ms));
        let mut continuous_tick = interval(Duration::from_millis(continuous_ms));

        loop {
            tokio::select! {
                command = receiver.recv() => {
                    match command {
                        Some(SessionCommand::Send(message)) => {
                            if let Err(error) = send_protocol(stream, &message).await {
                                return ConnectedExit::Disconnected(error.message);
                            }
                        }
                        Some(SessionCommand::Leave) => {
                            let _ = send_protocol(stream, &ProtocolMessage::new("leave_room", json!({}))).await;
                            let _ = stream.close(None).await;
                            return ConnectedExit::Left;
                        }
                        None => return ConnectedExit::Left,
                    }
                }
                incoming = stream.next() => {
                    match incoming {
                        Some(Ok(WebSocketMessage::Text(text))) => {
                            match serde_json::from_str::<ProtocolMessage>(&text) {
                                Ok(message) => {
                                    if message.version == PROTOCOL_VERSION {
                                        let _ = app.emit(MESSAGE_EVENT, &message);
                                    }
                                }
                                Err(error) => return ConnectedExit::Disconnected(error.to_string()),
                            }
                        }
                        Some(Ok(WebSocketMessage::Close(_))) | None => return ConnectedExit::Disconnected("server closed the connection".into()),
                        Some(Ok(_)) => {}
                        Some(Err(error)) => return ConnectedExit::Disconnected(error.to_string()),
                    }
                }
                _ = snapshot_tick.tick() => {
                    if let Some(message) = self.snapshot_message() {
                        if let Err(error) = send_protocol(stream, &message).await {
                            return ConnectedExit::Disconnected(error.message);
                        }
                    }
                }
                _ = continuous_tick.tick(), if policy.stream_mode == "limited" => {
                    if let Some(message) = self.continuous_message() {
                        if let Err(error) = send_protocol(stream, &message).await {
                            return ConnectedExit::Disconnected(error.message);
                        }
                    }
                }
            }
        }
    }

    pub fn publish_input(&self, kind: &str, value: Value) {
        self.update_input_state(kind, &value);
        let policy = self.inner.policy.read().unwrap().clone();
        if policy.stream_mode == "limited" && is_continuous(kind) {
            self.inner.input.lock().unwrap().continuous_dirty = true;
            return;
        }

        if !self
            .inner
            .rate
            .lock()
            .unwrap()
            .allow(policy.max_events_per_second)
        {
            return;
        }

        let event = self.new_event(kind, value);
        self.try_send(ProtocolMessage::new("input", json!({ "event": event })));
    }

    pub fn set_bounds(&self, bounds: DisplayBounds) {
        *self.inner.bounds.write().unwrap() = Some(bounds.clone());
        if let Some(cursor) = self.inner.input.lock().unwrap().cursor.as_mut() {
            cursor.bounds = Some(bounds);
        }
    }

    pub fn update_profile(&self, app: &AppHandle, skin_id: String, mode: String) {
        let updated = {
            let mut room = self.inner.room.write().unwrap();
            room.as_mut().map(|joined| {
                joined.self_player.skin_id = skin_id.clone();
                joined.self_player.mode = mode.clone();
                if let Some(player) = joined
                    .players
                    .iter_mut()
                    .find(|player| player.player_id == joined.self_player.player_id)
                {
                    player.skin_id = skin_id.clone();
                    player.mode = mode.clone();
                }
                joined.self_player.clone()
            })
        };
        self.try_send(ProtocolMessage::new(
            "profile_update",
            json!({ "skinId": skin_id, "mode": mode }),
        ));
        if let Some(player) = updated {
            self.emit_message(app, "member_updated", &json!({ "player": player }));
        }
    }

    fn update_input_state(&self, kind: &str, value: &Value) {
        let mut input = self.inner.input.lock().unwrap();
        match kind {
            "KeyboardPress" => {
                if let Some(key) = value.as_str() {
                    input.pressed_keys.insert(key.into());
                }
            }
            "KeyboardRelease" => {
                if let Some(key) = value.as_str() {
                    input.pressed_keys.remove(key);
                }
            }
            "MousePress" => {
                if let Some(button) = value.as_str() {
                    input.mouse_buttons.insert(button.into());
                }
            }
            "MouseRelease" => {
                if let Some(button) = value.as_str() {
                    input.mouse_buttons.remove(button);
                }
            }
            "MouseMove" => {
                if let (Some(x), Some(y)) = (
                    value.get("x").and_then(Value::as_f64),
                    value.get("y").and_then(Value::as_f64),
                ) {
                    input.cursor = Some(CursorState {
                        x,
                        y,
                        bounds: self.inner.bounds.read().unwrap().clone(),
                    });
                }
            }
            "AxisChanged" | "ButtonChanged" => {
                if let (Some(name), Some(axis_value)) = (
                    value.get("name").and_then(Value::as_str),
                    value.get("value").and_then(Value::as_f64),
                ) {
                    input.gamepad.insert(name.into(), axis_value);
                }
            }
            _ => {}
        }
    }

    fn new_event(&self, kind: &str, value: Value) -> InputEvent {
        InputEvent {
            sequence: self.inner.sequence.fetch_add(1, Ordering::SeqCst) + 1,
            client_time_ms: now_ms(),
            kind: kind.into(),
            value,
            bounds: self.inner.bounds.read().unwrap().clone(),
        }
    }

    fn continuous_message(&self) -> Option<ProtocolMessage> {
        let mut input = self.inner.input.lock().unwrap();
        if !input.continuous_dirty {
            return None;
        }
        input.continuous_dirty = false;
        let value = json!({ "cursor": input.cursor, "gamepad": input.gamepad });
        drop(input);
        let event = self.new_event("ContinuousState", value);
        Some(ProtocolMessage::new("input", json!({ "event": event })))
    }

    fn snapshot_message(&self) -> Option<ProtocolMessage> {
        if self.inner.sender.lock().unwrap().is_none() {
            return None;
        }
        let input = self.inner.input.lock().unwrap();
        let snapshot = InputSnapshot {
            sequence: self.inner.sequence.fetch_add(1, Ordering::SeqCst) + 1,
            client_time_ms: now_ms(),
            pressed_keys: input.pressed_keys.iter().cloned().collect(),
            mouse_buttons: input.mouse_buttons.iter().cloned().collect(),
            cursor: input.cursor.clone(),
            gamepad: input.gamepad.clone(),
        };
        Some(ProtocolMessage::new(
            "snapshot",
            json!({ "snapshot": snapshot }),
        ))
    }

    fn try_send(&self, message: ProtocolMessage) {
        let sender = self
            .inner
            .sender
            .lock()
            .unwrap()
            .as_ref()
            .map(|(_, sender)| sender.clone());
        if let Some(sender) = sender {
            let _ = sender.try_send(SessionCommand::Send(message));
        }
    }

    fn emit_message<T: Serialize>(&self, app: &AppHandle, message_type: &str, payload: &T) {
        let message = ProtocolMessage::new(
            message_type,
            serde_json::to_value(payload).unwrap_or(Value::Null),
        );
        let _ = app.emit(MESSAGE_EVENT, message);
    }

    fn emit_status(&self, app: &AppHandle, state: &str, error: Option<ProtocolError>) {
        let status = MultiplayerStatus {
            state: state.into(),
            room: self.inner.room.read().unwrap().clone(),
            error,
        };
        let _ = app.emit(STATUS_EVENT, status);
    }

    fn clear(&self, generation: u64) {
        let current = self
            .inner
            .sender
            .lock()
            .unwrap()
            .as_ref()
            .map(|(value, _)| *value);
        if current == Some(generation) {
            *self.inner.sender.lock().unwrap() = None;
            *self.inner.room.write().unwrap() = None;
        }
    }

    pub fn status(&self) -> MultiplayerStatus {
        MultiplayerStatus {
            state: if self.inner.room.read().unwrap().is_some() {
                "connected".into()
            } else {
                "disconnected".into()
            },
            room: self.inner.room.read().unwrap().clone(),
            error: None,
        }
    }
}

type WebSocketStream =
    tokio_tungstenite::WebSocketStream<tokio_tungstenite::MaybeTlsStream<tokio::net::TcpStream>>;

async fn connect_and_request(
    endpoint: &str,
    request: ProtocolMessage,
) -> Result<(WebSocketStream, RoomJoined, RoomPolicy), ProtocolError> {
    let (mut stream, _) = connect_async(endpoint)
        .await
        .map_err(|error| ProtocolError::connection(error.to_string()))?;

    let policy_message = read_protocol(&mut stream).await?;
    if policy_message.message_type != "policy" {
        return Err(ProtocolError::connection(
            "server did not provide a room policy",
        ));
    }
    let policy: RoomPolicy = serde_json::from_value(policy_message.payload)
        .map_err(|error| ProtocolError::connection(error.to_string()))?;

    send_protocol(&mut stream, &request).await?;
    loop {
        let message = read_protocol(&mut stream).await?;
        match message.message_type.as_str() {
            "room_joined" => {
                let joined = serde_json::from_value::<RoomJoined>(message.payload)
                    .map_err(|error| ProtocolError::connection(error.to_string()))?;
                return Ok((stream, joined, policy));
            }
            "error" => {
                return Err(serde_json::from_value(message.payload)
                    .unwrap_or_else(|_| ProtocolError::connection("server rejected the request")));
            }
            _ => {}
        }
    }
}

async fn read_protocol(stream: &mut WebSocketStream) -> Result<ProtocolMessage, ProtocolError> {
    loop {
        let message = stream
            .next()
            .await
            .ok_or_else(|| ProtocolError::connection("server closed the connection"))?
            .map_err(|error| ProtocolError::connection(error.to_string()))?;
        match message {
            WebSocketMessage::Text(text) => {
                let protocol: ProtocolMessage = serde_json::from_str(&text)
                    .map_err(|error| ProtocolError::connection(error.to_string()))?;
                if protocol.version != PROTOCOL_VERSION {
                    return Err(ProtocolError::connection("unsupported protocol version"));
                }
                return Ok(protocol);
            }
            WebSocketMessage::Close(_) => {
                return Err(ProtocolError::connection("server closed the connection"));
            }
            _ => {}
        }
    }
}

async fn send_protocol(
    stream: &mut WebSocketStream,
    message: &ProtocolMessage,
) -> Result<(), ProtocolError> {
    let text = serde_json::to_string(message)
        .map_err(|error| ProtocolError::connection(error.to_string()))?;
    stream
        .send(WebSocketMessage::Text(text.into()))
        .await
        .map_err(|error| ProtocolError::connection(error.to_string()))
}

fn is_continuous(kind: &str) -> bool {
    matches!(kind, "MouseMove" | "AxisChanged" | "ContinuousState")
}

fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}

fn validate_endpoint(endpoint: &str) -> Result<(), ProtocolError> {
    let parsed = Url::parse(endpoint)
        .map_err(|error| ProtocolError::connection(format!("invalid server address: {error}")))?;
    if !matches!(parsed.scheme(), "ws" | "wss") {
        return Err(ProtocolError::connection(
            "server address must use WS or WSS",
        ));
    }
    let host = parsed.host_str().unwrap_or_default();
    let local = matches!(host, "localhost" | "127.0.0.1" | "::1");
    if parsed.scheme() == "ws" && !local {
        return Err(ProtocolError::connection(
            "remote server addresses must use WSS",
        ));
    }
    Ok(())
}

#[command]
pub async fn multiplayer_create_room(
    app: AppHandle,
    manager: State<'_, MultiplayerManager>,
    endpoint: String,
    profile: JoinProfile,
) -> Result<RoomJoined, ProtocolError> {
    manager
        .start(app, endpoint, InitialAction::Create(profile))
        .await
}

#[command]
pub async fn multiplayer_join_room(
    app: AppHandle,
    manager: State<'_, MultiplayerManager>,
    endpoint: String,
    room_code: String,
    profile: JoinProfile,
) -> Result<RoomJoined, ProtocolError> {
    manager
        .start(app, endpoint, InitialAction::Join(room_code, profile))
        .await
}

#[command]
pub async fn multiplayer_leave_room(
    app: AppHandle,
    manager: State<'_, MultiplayerManager>,
) -> Result<(), String> {
    manager.stop().await;
    manager.emit_status(&app, "disconnected", None);
    Ok(())
}

#[command]
pub fn multiplayer_status(manager: State<'_, MultiplayerManager>) -> MultiplayerStatus {
    manager.status()
}

#[command]
pub fn multiplayer_update_bounds(manager: State<'_, MultiplayerManager>, bounds: DisplayBounds) {
    manager.set_bounds(bounds);
}

#[command]
pub fn multiplayer_update_profile(
    app: AppHandle,
    manager: State<'_, MultiplayerManager>,
    skin_id: String,
    mode: String,
) {
    manager.update_profile(&app, skin_id, mode);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn local_rate_resets_each_second() {
        let mut rate = LocalRate::default();
        assert!(rate.allow(1));
        assert!(!rate.allow(1));
        rate.window = Instant::now() - Duration::from_secs(2);
        assert!(rate.allow(1));
    }

    #[test]
    fn continuous_kinds_are_classified() {
        assert!(is_continuous("MouseMove"));
        assert!(is_continuous("AxisChanged"));
        assert!(!is_continuous("KeyboardPress"));
    }

    #[test]
    fn limited_mode_coalesces_continuous_input() {
        let manager = MultiplayerManager::default();
        manager.inner.policy.write().unwrap().stream_mode = "limited".into();
        manager.publish_input("MouseMove", json!({ "x": 10.0, "y": 20.0 }));

        let message = manager.continuous_message().expect("continuous state");
        assert_eq!(message.message_type, "input");
        assert!(manager.continuous_message().is_none());
    }

    #[test]
    fn snapshot_repairs_pressed_state() {
        let manager = MultiplayerManager::default();
        let (sender, _receiver) = mpsc::channel(1);
        *manager.inner.sender.lock().unwrap() = Some((1, sender));
        manager.publish_input("KeyboardPress", json!("A"));
        manager.publish_input("MousePress", json!("Left"));

        let message = manager.snapshot_message().expect("snapshot");
        let snapshot: InputSnapshot =
            serde_json::from_value(message.payload["snapshot"].clone()).unwrap();
        assert!(snapshot.pressed_keys.contains(&"A".to_string()));
        assert!(snapshot.mouse_buttons.contains(&"Left".to_string()));
    }

    #[test]
    fn remote_endpoints_require_wss() {
        assert!(validate_endpoint("ws://127.0.0.1:8080/v1/ws").is_ok());
        assert!(validate_endpoint("wss://example.com/v1/ws").is_ok());
        assert!(validate_endpoint("ws://example.com/v1/ws").is_err());
    }
}
