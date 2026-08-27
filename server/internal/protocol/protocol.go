package protocol

import (
	"encoding/json"
	"fmt"
)

const Version = "v1"

const (
	TypePolicy        = "policy"
	TypeCreateRoom    = "create_room"
	TypeJoinRoom      = "join_room"
	TypeResumeRoom    = "resume_room"
	TypeLeaveRoom     = "leave_room"
	TypeRoomJoined    = "room_joined"
	TypeMemberJoined  = "member_joined"
	TypeMemberUpdated = "member_updated"
	TypeMemberLeft    = "member_left"
	TypeMemberLatency = "member_latency"
	TypeInput         = "input"
	TypeSnapshot      = "snapshot"
	TypeProfileUpdate = "profile_update"
	TypeError         = "error"
)

const (
	ErrorAlreadyJoined   = "ALREADY_JOINED"
	ErrorInvalidMessage  = "INVALID_MESSAGE"
	ErrorInvalidName     = "INVALID_NAME"
	ErrorNameTaken       = "NAME_TAKEN"
	ErrorNotJoined       = "NOT_JOINED"
	ErrorRateLimited     = "RATE_LIMITED"
	ErrorRoomFull        = "ROOM_FULL"
	ErrorRoomLimit       = "ROOM_LIMIT_REACHED"
	ErrorRoomNotFound    = "ROOM_NOT_FOUND"
	ErrorSessionExpired  = "SESSION_EXPIRED"
	ErrorUnsupportedMode = "UNSUPPORTED_MODE"
)

const (
	StreamModeRaw     = "raw"
	StreamModeLimited = "limited"
)

const (
	ModelModeStandard = "standard"
	ModelModeKeyboard = "keyboard"
	ModelModeGamepad  = "gamepad"
)

type Message struct {
	Version   string          `json:"v"`
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func NewMessage(messageType, requestID string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return json.Marshal(Message{
		Version:   Version,
		Type:      messageType,
		RequestID: requestID,
		Payload:   raw,
	})
}

func DecodePayload[T any](message Message) (T, error) {
	var value T
	if len(message.Payload) == 0 {
		return value, fmt.Errorf("message payload is required")
	}

	if err := json.Unmarshal(message.Payload, &value); err != nil {
		return value, fmt.Errorf("decode payload: %w", err)
	}

	return value, nil
}

type RoomPolicy struct {
	StreamMode        string `json:"streamMode"`
	MaxPlayers        int    `json:"maxPlayers"`
	MaxEventsPerSec   int    `json:"maxEventsPerSecond"`
	ContinuousHz      int    `json:"continuousHz"`
	MaxMessageBytes   int64  `json:"maxMessageBytes"`
	SnapshotIntervalM int    `json:"snapshotIntervalMs"`
}

type PlayerProfile struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	SkinID   string `json:"skinId"`
	Mode     string `json:"mode"`
	Order    uint64 `json:"order"`
	Online   bool   `json:"online"`
}

type JoinProfile struct {
	Name   string `json:"name"`
	SkinID string `json:"skinId"`
	Mode   string `json:"mode"`
}

type CreateRoomRequest struct {
	Profile JoinProfile `json:"profile"`
}

type JoinRoomRequest struct {
	RoomCode string      `json:"roomCode"`
	Profile  JoinProfile `json:"profile"`
}

type ResumeRoomRequest struct {
	RoomCode    string `json:"roomCode"`
	PlayerID    string `json:"playerId"`
	ResumeToken string `json:"resumeToken"`
}

type RoomJoined struct {
	RoomCode    string          `json:"roomCode"`
	Self        PlayerProfile   `json:"self"`
	Players     []PlayerProfile `json:"players"`
	ResumeToken string          `json:"resumeToken"`
	Policy      RoomPolicy      `json:"policy"`
}

type MemberPayload struct {
	Player PlayerProfile `json:"player"`
}

type MemberLeft struct {
	PlayerID string `json:"playerId"`
}

type MemberLatencyPayload struct {
	PlayerID  string `json:"playerId"`
	LatencyMS *int64 `json:"latencyMs"`
}

type ProfileUpdate struct {
	SkinID string `json:"skinId"`
	Mode   string `json:"mode"`
}

type DisplayBounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type InputEvent struct {
	Sequence     uint64          `json:"sequence"`
	ClientTimeMs int64           `json:"clientTimeMs"`
	Kind         string          `json:"kind"`
	Value        json.RawMessage `json:"value"`
	Bounds       *DisplayBounds  `json:"bounds,omitempty"`
}

type InputEventPayload struct {
	PlayerID string     `json:"playerId,omitempty"`
	Event    InputEvent `json:"event"`
}

type CursorState struct {
	X      float64        `json:"x"`
	Y      float64        `json:"y"`
	Bounds *DisplayBounds `json:"bounds,omitempty"`
}

type InputSnapshot struct {
	Sequence     uint64             `json:"sequence"`
	ClientTimeMs int64              `json:"clientTimeMs"`
	PressedKeys  []string           `json:"pressedKeys"`
	MouseButtons []string           `json:"mouseButtons"`
	Cursor       *CursorState       `json:"cursor,omitempty"`
	Gamepad      map[string]float64 `json:"gamepad"`
}

type SnapshotPayload struct {
	PlayerID string        `json:"playerId,omitempty"`
	Snapshot InputSnapshot `json:"snapshot"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func IsModelMode(value string) bool {
	return value == ModelModeStandard || value == ModelModeKeyboard || value == ModelModeGamepad
}

func IsContinuousEvent(kind string) bool {
	return kind == "MouseMove" || kind == "AxisChanged" || kind == "ContinuousState"
}

func IsInputEvent(kind string) bool {
	switch kind {
	case "MousePress", "MouseRelease", "MouseMove", "KeyboardPress", "KeyboardRelease", "ButtonChanged", "AxisChanged", "ContinuousState":
		return true
	default:
		return false
	}
}
