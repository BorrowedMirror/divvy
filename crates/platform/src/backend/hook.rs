//! The ONE process-wide passive input hook (`rdev`), shared by the X11, Windows and macOS
//! backends. The rdev callback only does a non-blocking `try_send` into a bounded channel:
//! hook callbacks run on OS input paths (low-level hooks on Windows, event taps on macOS)
//! and must never block or allocate unboundedly.
//!
//! Only keyboard events are hooked: rdev's `KEYBOARD_ONLY` switch keeps it from installing a
//! low-level mouse hook (`WH_MOUSE_LL` on Windows, mouse event-tap masks on macOS, pointer
//! events in the XRecord range), which would add latency to every mouse move in the game.
use std::sync::Mutex;
use std::sync::mpsc::{Receiver, SyncSender, TrySendError, sync_channel};

use crate::{InputEvent, PlatformError};

/// Events buffered before new ones are dropped (consumer stalled).
pub(crate) const HOOK_CHANNEL_CAPACITY: usize = 1024;

/// The environment variable the rustdesk-org rdev fork reads to hook the keyboard only.
pub const KEYBOARD_ONLY_ENV: &str = "KEYBOARD_ONLY";

/// Set `KEYBOARD_ONLY` for rdev. Call it from `main` before any other thread exists; the
/// hook calls it again (a no-op once set) as a fallback.
pub fn init_process_env() {
    if std::env::var_os(KEYBOARD_ONLY_ENV).is_none() {
        // SAFETY: rdev exposes this switch only as an environment variable. Writing the
        // environment races with concurrent `getenv` in other threads; callers are told to run
        // this from `main` before spawning threads, and it writes at most once per process.
        unsafe { std::env::set_var(KEYBOARD_ONLY_ENV, "1") };
    }
}

/// `true` while a hook thread is running (or starting). A `Mutex<bool>` rather than a
/// `OnceLock` so a hook that failed to start (no X server, missing permission) can be retried.
static STARTED: Mutex<bool> = Mutex::new(false);

/// Start the global hook. Returns `AlreadyStarted` on a second call while the first hook lives.
///
/// Never blocks: the hook starts on its own thread. A late start failure (no RECORD extension,
/// missing permission) is logged and reported by the returned receiver disconnecting; the hook
/// can then be started again.
pub(crate) fn start_global_hook() -> Result<Receiver<InputEvent>, PlatformError> {
    {
        let mut started = STARTED.lock().unwrap_or_else(std::sync::PoisonError::into_inner);
        if *started {
            return Err(PlatformError::AlreadyStarted);
        }
        *started = true;
    }
    init_process_env();
    let (tx, rx) = sync_channel(HOOK_CHANNEL_CAPACITY);
    let spawned = std::thread::Builder::new().name("rkl-input-hook".into()).spawn(move || {
        // rdev::listen blocks for the life of the hook; it only returns on failure.
        let result = rdev::listen(move |event| {
            if let Some(ev) = map_event(&event.event_type) {
                forward(&tx, ev);
            }
        });
        mark_stopped();
        if let Err(e) = result {
            tracing::warn!(error = ?e, "input hook failed to start");
        }
    });
    if let Err(e) = spawned {
        mark_stopped();
        return Err(PlatformError::Backend(format!("spawn input hook thread: {e}")));
    }
    Ok(rx)
}

fn mark_stopped() {
    *STARTED.lock().unwrap_or_else(std::sync::PoisonError::into_inner) = false;
}

/// Non-blocking hand-off; drops the event when the consumer lags or has gone away.
pub(crate) fn forward(tx: &SyncSender<InputEvent>, ev: InputEvent) -> bool {
    match tx.try_send(ev) {
        Ok(()) => true,
        Err(TrySendError::Full(_) | TrySendError::Disconnected(_)) => false,
    }
}

/// Translate an rdev event. Mouse buttons are not needed by the app and are dropped.
pub(crate) fn map_event(ev: &rdev::EventType) -> Option<InputEvent> {
    use rdev::EventType as E;
    Some(match ev {
        E::KeyPress(k) => InputEvent::KeyDown { key: key_code(*k) },
        E::KeyRelease(k) => InputEvent::KeyUp { key: key_code(*k) },
        E::MouseMove { x, y } => InputEvent::MouseMove { x: *x as i32, y: *y as i32 },
        E::Wheel { delta_y, .. } if *delta_y != 0 => InputEvent::Wheel { delta: *delta_y as i32 },
        E::Wheel { .. } | E::ButtonPress(_) | E::ButtonRelease(_) => return None,
    })
}

/// rdev key -> W3C `KeyboardEvent.code` name (the format [`crate::Chord`] uses).
///
/// rdev derives its `Key` from the platform's physical key code on every OS (X11 keycode,
/// Windows scan/virtual key, macOS `kVK_*`), so these names are layout independent. Keys W3C
/// has no name for in rdev's table fall back to their rdev `Debug` name.
pub(crate) fn key_code(key: rdev::Key) -> String {
    use rdev::Key as K;
    if let Some(code) = rdev::chrome_keycode_from_key(key).filter(|c| !c.is_empty()) {
        return code.to_owned();
    }
    let named = match key {
        K::Function => "Fn",
        K::Pause => "Pause",
        K::Help => "Help",
        K::Unknown(_) => "Unidentified",
        other => return format!("{other:?}"),
    };
    named.to_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_keys_to_w3c_codes() {
        assert_eq!(key_code(rdev::Key::KeyC), "KeyC");
        assert_eq!(key_code(rdev::Key::Num7), "Digit7");
        assert_eq!(key_code(rdev::Key::F5), "F5");
        assert_eq!(key_code(rdev::Key::Escape), "Escape");
        assert_eq!(key_code(rdev::Key::ControlLeft), "ControlLeft");
        assert_eq!(key_code(rdev::Key::Return), "Enter");
        assert_eq!(key_code(rdev::Key::Kp0), "Numpad0");
        assert_eq!(key_code(rdev::Key::KpReturn), "NumpadEnter");
        assert_eq!(key_code(rdev::Key::F20), "F20");
        assert_eq!(key_code(rdev::Key::Function), "Fn");
        assert_eq!(key_code(rdev::Key::Pause), "Pause");
        assert_eq!(key_code(rdev::Key::Unknown(999)), "Unidentified");
        assert_eq!(key_code(rdev::Key::Kana), "Kana");
    }

    /// Named keys come straight from rdev's W3C table and map back to the same key.
    #[test]
    fn named_keys_round_trip_through_rdev_table() {
        use rdev::Key as K;
        for key in [K::Escape, K::KeyA, K::Num0, K::F1, K::Dot, K::BackQuote, K::Insert, K::Kp9, K::IntlBackslash] {
            let code = key_code(key);
            assert_eq!(rdev::chrome_key_from_code(&code), key, "{code}");
        }
    }

    #[test]
    fn maps_events() {
        use rdev::EventType as E;
        assert_eq!(map_event(&E::KeyPress(rdev::Key::KeyA)), Some(InputEvent::KeyDown { key: "KeyA".into() }));
        assert_eq!(map_event(&E::KeyRelease(rdev::Key::KeyA)), Some(InputEvent::KeyUp { key: "KeyA".into() }));
        assert_eq!(map_event(&E::MouseMove { x: 10.6, y: -2.0 }), Some(InputEvent::MouseMove { x: 10, y: -2 }));
        assert_eq!(map_event(&E::Wheel { delta_x: 0, delta_y: -1 }), Some(InputEvent::Wheel { delta: -1 }));
        assert_eq!(map_event(&E::Wheel { delta_x: 1, delta_y: 0 }), None);
        assert_eq!(map_event(&E::ButtonPress(rdev::Button::Left)), None);
    }

    #[test]
    fn forward_never_blocks() {
        let (tx, rx) = sync_channel(1);
        assert!(forward(&tx, InputEvent::Wheel { delta: 1 }));
        assert!(!forward(&tx, InputEvent::Wheel { delta: 2 }), "full channel drops");
        drop(rx);
        assert!(!forward(&tx, InputEvent::Wheel { delta: 3 }), "closed channel drops");
    }
}
