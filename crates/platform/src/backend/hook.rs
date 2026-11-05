//! The ONE process-wide passive input hook (`rdev`), shared by the X11, Windows and macOS
//! backends. The rdev callback only does a non-blocking `try_send` into a bounded channel:
//! hook callbacks run on OS input paths (low-level hooks on Windows, event taps on macOS)
//! and must never block or allocate unboundedly.
use std::sync::Mutex;
use std::sync::mpsc::{Receiver, SyncSender, TryRecvError, TrySendError, sync_channel};
use std::time::Duration;

use crate::{InputEvent, PlatformError};

/// Events buffered before new ones are dropped (consumer stalled).
pub(crate) const HOOK_CHANNEL_CAPACITY: usize = 1024;

/// How long `start_global_hook` waits for rdev to report an early start failure.
const START_GRACE: Duration = Duration::from_millis(250);

/// `true` while a hook thread is running (or starting). A `Mutex<bool>` rather than a
/// `OnceLock` so a hook that failed to start (no X server, missing permission) can be retried.
static STARTED: Mutex<bool> = Mutex::new(false);

/// Start the global hook. Returns `AlreadyStarted` on a second call while the first hook lives.
pub(crate) fn start_global_hook() -> Result<Receiver<InputEvent>, PlatformError> {
    {
        let mut started = STARTED.lock().unwrap_or_else(std::sync::PoisonError::into_inner);
        if *started {
            return Err(PlatformError::AlreadyStarted);
        }
        *started = true;
    }
    let (tx, rx) = sync_channel(HOOK_CHANNEL_CAPACITY);
    let (err_tx, err_rx) = sync_channel::<String>(1);
    let spawned = std::thread::Builder::new().name("kalandra-input-hook".into()).spawn(move || {
        let result = rdev::listen(move |event| {
            if let Some(ev) = map_event(&event.event_type) {
                forward(&tx, ev);
            }
        });
        if let Err(e) = result {
            mark_stopped();
            let _ = err_tx.try_send(format!("{e:?}"));
        }
    });
    if let Err(e) = spawned {
        mark_stopped();
        return Err(PlatformError::Backend(format!("spawn input hook thread: {e}")));
    }
    // rdev::listen blocks forever on success; failures (no display, no RECORD extension,
    // missing accessibility permission) are reported immediately.
    std::thread::sleep(START_GRACE);
    match err_rx.try_recv() {
        Ok(msg) => Err(PlatformError::Backend(format!("input hook: {msg}"))),
        Err(TryRecvError::Empty | TryRecvError::Disconnected) => Ok(rx),
    }
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
pub(crate) fn key_code(key: rdev::Key) -> String {
    use rdev::Key as K;
    let named = match key {
        K::Alt => "AltLeft",
        K::AltGr => "AltRight",
        K::Backspace => "Backspace",
        K::CapsLock => "CapsLock",
        K::ControlLeft => "ControlLeft",
        K::ControlRight => "ControlRight",
        K::Delete => "Delete",
        K::DownArrow => "ArrowDown",
        K::UpArrow => "ArrowUp",
        K::LeftArrow => "ArrowLeft",
        K::RightArrow => "ArrowRight",
        K::End => "End",
        K::Escape => "Escape",
        K::Home => "Home",
        K::PageDown => "PageDown",
        K::PageUp => "PageUp",
        K::Return => "Enter",
        K::ShiftLeft => "ShiftLeft",
        K::ShiftRight => "ShiftRight",
        K::Space => "Space",
        K::Tab => "Tab",
        K::MetaLeft => "MetaLeft",
        K::MetaRight => "MetaRight",
        K::Insert => "Insert",
        K::Minus => "Minus",
        K::Equal => "Equal",
        K::LeftBracket => "BracketLeft",
        K::RightBracket => "BracketRight",
        K::SemiColon => "Semicolon",
        K::Quote => "Quote",
        K::BackQuote => "Backquote",
        K::BackSlash => "Backslash",
        K::Comma => "Comma",
        K::Dot => "Period",
        K::Slash => "Slash",
        other => {
            let dbg = format!("{other:?}");
            // KeyA..KeyZ and F1..F24 already match W3C names; Num0..Num9 -> Digit0..Digit9.
            return match dbg.strip_prefix("Num") {
                Some(d) if d.len() == 1 && d.chars().all(|c| c.is_ascii_digit()) => format!("Digit{d}"),
                _ => dbg,
            };
        }
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
