use rdev::{Event, EventType, listen};
use serde::Serialize;
use serde_json::{Value, json};
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager, Runtime, command};

use crate::core::multiplayer::{DisplayBounds, MultiplayerManager};

const CAPS_LOCK_RELEASE_DELAY: Duration = Duration::from_millis(100);

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
pub enum DeviceEventKind {
    MousePress,
    MouseRelease,
    MouseMove,
    KeyboardPress,
    KeyboardRelease,
}

#[derive(Debug, Clone, Serialize)]
pub struct DeviceEvent {
    kind: DeviceEventKind,
    value: Value,
}

fn caps_lock_auto_release(event: &DeviceEvent) -> Option<DeviceEvent> {
    (event.kind == DeviceEventKind::KeyboardPress && event.value.as_str() == Some("CapsLock")).then(
        || DeviceEvent {
            kind: DeviceEventKind::KeyboardRelease,
            value: event.value.clone(),
        },
    )
}

fn dispatch_device_event<R: Runtime>(
    app_handle: &AppHandle<R>,
    device_event: DeviceEvent,
    display_bounds: Option<DisplayBounds>,
) {
    let manager = app_handle.state::<MultiplayerManager>();
    if let Some(bounds) = display_bounds {
        manager.set_bounds(bounds);
    }

    let _ = app_handle.emit("device-changed", device_event.clone());
    manager.publish_input(&format!("{:?}", device_event.kind), device_event.value);
}

static IS_LISTENING: AtomicBool = AtomicBool::new(false);

fn find_display_bounds(
    x: f64,
    y: f64,
    bounds: impl IntoIterator<Item = DisplayBounds>,
) -> Option<DisplayBounds> {
    bounds.into_iter().find(|bounds| {
        x >= bounds.x
            && x < bounds.x + bounds.width
            && y >= bounds.y
            && y < bounds.y + bounds.height
    })
}

fn cursor_and_display_bounds<R: Runtime>(
    app_handle: &AppHandle<R>,
    fallback_x: f64,
    fallback_y: f64,
) -> (f64, f64, Option<DisplayBounds>) {
    let cursor = app_handle.cursor_position().ok();
    let x = cursor.as_ref().map_or(fallback_x, |cursor| cursor.x);
    let y = cursor.as_ref().map_or(fallback_y, |cursor| cursor.y);

    let bounds = app_handle.available_monitors().ok().and_then(|monitors| {
        find_display_bounds(
            x,
            y,
            monitors.into_iter().map(|monitor| DisplayBounds {
                x: monitor.position().x as f64,
                y: monitor.position().y as f64,
                width: monitor.size().width as f64,
                height: monitor.size().height as f64,
            }),
        )
    });

    (x, y, bounds)
}

#[command]
pub async fn start_device_listening<R: Runtime>(app_handle: AppHandle<R>) -> Result<(), String> {
    if IS_LISTENING.load(Ordering::SeqCst) {
        return Ok(());
    }

    IS_LISTENING.store(true, Ordering::SeqCst);

    let callback = move |event: Event| {
        let (device_event, display_bounds) = match event.event_type {
            EventType::ButtonPress(button) => (
                DeviceEvent {
                    kind: DeviceEventKind::MousePress,
                    value: json!(format!("{:?}", button)),
                },
                None,
            ),
            EventType::ButtonRelease(button) => (
                DeviceEvent {
                    kind: DeviceEventKind::MouseRelease,
                    value: json!(format!("{:?}", button)),
                },
                None,
            ),
            EventType::MouseMove { x, y } => {
                let (x, y, bounds) = cursor_and_display_bounds(&app_handle, x, y);

                (
                    DeviceEvent {
                        kind: DeviceEventKind::MouseMove,
                        value: json!({ "x": x, "y": y }),
                    },
                    bounds,
                )
            }
            EventType::KeyPress(key) => (
                DeviceEvent {
                    kind: DeviceEventKind::KeyboardPress,
                    value: json!(format!("{:?}", key)),
                },
                None,
            ),
            EventType::KeyRelease(key) => (
                DeviceEvent {
                    kind: DeviceEventKind::KeyboardRelease,
                    value: json!(format!("{:?}", key)),
                },
                None,
            ),
            _ => return,
        };

        let auto_release = caps_lock_auto_release(&device_event);
        dispatch_device_event(&app_handle, device_event, display_bounds);

        if let Some(release_event) = auto_release {
            let app_handle = app_handle.clone();
            thread::spawn(move || {
                thread::sleep(CAPS_LOCK_RELEASE_DELAY);
                dispatch_device_event(&app_handle, release_event, None);
            });
        }
    };

    if let Err(err) = listen(callback) {
        IS_LISTENING.store(false, Ordering::SeqCst);
        return Err(format!("Failed to listen device: {:?}", err));
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn finds_bounds_for_negative_monitor_coordinates() {
        let bounds = find_display_bounds(
            -100.0,
            100.0,
            [
                DisplayBounds {
                    x: 0.0,
                    y: 0.0,
                    width: 1920.0,
                    height: 1080.0,
                },
                DisplayBounds {
                    x: -1280.0,
                    y: 0.0,
                    width: 1280.0,
                    height: 1024.0,
                },
            ],
        )
        .expect("cursor should be on the left monitor");

        assert_eq!(bounds.x, -1280.0);
        assert_eq!(bounds.width, 1280.0);
    }

    #[test]
    fn treats_monitor_edges_as_half_open() {
        let bounds = find_display_bounds(
            1920.0,
            100.0,
            [
                DisplayBounds {
                    x: 0.0,
                    y: 0.0,
                    width: 1920.0,
                    height: 1080.0,
                },
                DisplayBounds {
                    x: 1920.0,
                    y: 0.0,
                    width: 1920.0,
                    height: 1080.0,
                },
            ],
        )
        .expect("cursor should be on the right monitor");

        assert_eq!(bounds.x, 1920.0);
    }

    #[test]
    fn caps_lock_press_gets_a_synthetic_release() {
        let event = DeviceEvent {
            kind: DeviceEventKind::KeyboardPress,
            value: json!("CapsLock"),
        };

        let release = caps_lock_auto_release(&event).expect("CapsLock should auto release");
        assert_eq!(release.kind, DeviceEventKind::KeyboardRelease);
        assert_eq!(release.value, json!("CapsLock"));
    }

    #[test]
    fn ordinary_keys_and_release_events_are_not_synthesized() {
        for event in [
            DeviceEvent {
                kind: DeviceEventKind::KeyboardPress,
                value: json!("KeyA"),
            },
            DeviceEvent {
                kind: DeviceEventKind::KeyboardRelease,
                value: json!("CapsLock"),
            },
        ] {
            assert!(caps_lock_auto_release(&event).is_none());
        }
    }
}
