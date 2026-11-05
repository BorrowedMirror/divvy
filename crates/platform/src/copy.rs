//! The user-triggered copy flow: send Ctrl+C (or Ctrl+Alt+C), poll the clipboard, restore.
use std::time::Duration;

use crate::{Chord, Clipboard, InputSimulator, KeyModifier, PlatformError};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CopyOptions {
    pub advanced: bool,
    pub timeout: Duration,
    pub poll_interval: Duration,
    pub restore_clipboard: bool,
}

impl Default for CopyOptions {
    fn default() -> Self {
        Self {
            advanced: true,
            timeout: Duration::from_millis(500),
            poll_interval: Duration::from_millis(15),
            restore_clipboard: true,
        }
    }
}

/// Header every PoE2 item copy starts with. Kept local so this crate never depends on core.
const ITEM_HEADER: &str = "Item Class:";

/// Cheap check equivalent to `kalandra_core::parser::looks_like_item`.
pub fn looks_like_item(text: &str) -> bool {
    text.trim_start().starts_with(ITEM_HEADER)
}

/// The chord PoE2 uses: `Ctrl+Alt+C` for the advanced (mod tiers) format, else `Ctrl+C`.
pub fn copy_chord(advanced: bool) -> Chord {
    if advanced {
        Chord::new(&[KeyModifier::Ctrl, KeyModifier::Alt], "KeyC")
    } else {
        Chord::new(&[KeyModifier::Ctrl], "KeyC")
    }
}

/// Send the copy chord and return the item text. Clears the clipboard first so a stale
/// item is never returned; restores the prior clipboard text afterwards when asked.
/// `sleep` is injected so tests run instantly.
///
/// Errors: [`PlatformError::Timeout`] when nothing arrived, [`PlatformError::NoItemText`] when
/// the clipboard changed but to something that is not an item; clipboard/simulator errors from
/// clearing or sending the chord are passed through (after restoring).
pub fn copy_item_text(
    sim: &dyn InputSimulator,
    clip: &dyn Clipboard,
    opts: CopyOptions,
    sleep: &dyn Fn(Duration),
) -> Result<String, PlatformError> {
    // A clipboard that cannot be read (e.g. holds an image) is treated as empty.
    let previous = if opts.restore_clipboard { clip.get_text().ok().flatten() } else { None };
    let result =
        clip.clear().and_then(|()| sim.send_chord(&copy_chord(opts.advanced))).and_then(|()| poll(clip, opts, sleep));
    if opts.restore_clipboard {
        let restored = match &previous {
            Some(text) => clip.set_text(text),
            None => clip.clear(),
        };
        if let Err(e) = restored {
            tracing::warn!(error = %e, "failed to restore clipboard");
        }
    }
    result
}

fn poll(clip: &dyn Clipboard, opts: CopyOptions, sleep: &dyn Fn(Duration)) -> Result<String, PlatformError> {
    let step = opts.poll_interval.max(Duration::from_millis(1));
    let mut waited = Duration::ZERO;
    let mut saw_other_text = false;
    loop {
        // Transient read errors while the game is still writing are not fatal.
        if let Ok(Some(text)) = clip.get_text() {
            if looks_like_item(&text) {
                return Ok(text);
            }
            saw_other_text |= !text.is_empty();
        }
        if waited >= opts.timeout {
            return Err(if saw_other_text { PlatformError::NoItemText } else { PlatformError::Timeout(opts.timeout) });
        }
        sleep(step);
        waited += step;
    }
}

#[cfg(test)]
mod tests {
    use std::cell::Cell;
    use std::sync::{Arc, Mutex};

    use mockall::predicate::eq;

    use super::*;
    use crate::{MockClipboard, MockInputSimulator};

    const ITEM: &str = "Item Class: Rings\nRarity: Rare\n";

    type State = Arc<Mutex<Option<String>>>;

    /// A clipboard mock backed by shared state so the simulator can "write" to it.
    fn stateful_clip(state: &State) -> MockClipboard {
        let mut clip = MockClipboard::new();
        let s = Arc::clone(state);
        clip.expect_get_text().returning(move || Ok(s.lock().unwrap().clone()));
        let s = Arc::clone(state);
        clip.expect_set_text().returning(move |t| {
            *s.lock().unwrap() = Some(t.to_owned());
            Ok(())
        });
        let s = Arc::clone(state);
        clip.expect_clear().returning(move || {
            *s.lock().unwrap() = None;
            Ok(())
        });
        clip
    }

    fn opts() -> CopyOptions {
        CopyOptions {
            timeout: Duration::from_millis(100),
            poll_interval: Duration::from_millis(10),
            ..CopyOptions::default()
        }
    }

    #[test]
    fn looks_like_item_checks_header() {
        assert!(looks_like_item("\r\n  Item Class: Rings"));
        assert!(!looks_like_item("hello"));
        assert!(!looks_like_item(""));
    }

    #[test]
    fn chords() {
        assert_eq!(copy_chord(true).to_string(), "Ctrl+Alt+C");
        assert_eq!(copy_chord(false).to_string(), "Ctrl+C");
    }

    #[test]
    fn advanced_copy_returns_item_and_restores_previous_text() {
        let state = Arc::new(Mutex::new(Some("previous".to_owned())));
        let clip = stateful_clip(&state);
        let mut sim = MockInputSimulator::new();
        let s = Arc::clone(&state);
        sim.expect_send_chord().with(eq(copy_chord(true))).times(1).returning(move |_| {
            assert_eq!(*s.lock().unwrap(), None, "clipboard must be cleared before sending");
            *s.lock().unwrap() = Some(ITEM.to_owned());
            Ok(())
        });
        let text = copy_item_text(&sim, &clip, opts(), &|_| {}).unwrap();
        assert_eq!(text, ITEM);
        assert_eq!(state.lock().unwrap().as_deref(), Some("previous"));
    }

    #[test]
    fn plain_copy_without_restore_keeps_item_on_clipboard() {
        let state = Arc::new(Mutex::new(Some("previous".to_owned())));
        let clip = stateful_clip(&state);
        let mut sim = MockInputSimulator::new();
        let s = Arc::clone(&state);
        sim.expect_send_chord().with(eq(copy_chord(false))).returning(move |_| {
            *s.lock().unwrap() = Some(ITEM.to_owned());
            Ok(())
        });
        let o = CopyOptions { advanced: false, restore_clipboard: false, ..opts() };
        assert_eq!(copy_item_text(&sim, &clip, o, &|_| {}).unwrap(), ITEM);
        assert_eq!(state.lock().unwrap().as_deref(), Some(ITEM));
    }

    #[test]
    fn item_arriving_late_is_picked_up_by_polling() {
        let state: State = Arc::new(Mutex::new(None));
        let clip = stateful_clip(&state);
        let mut sim = MockInputSimulator::new();
        sim.expect_send_chord().returning(|_| Ok(()));
        let sleeps = Cell::new(0u32);
        let s = Arc::clone(&state);
        let sleep = move |d: Duration| {
            assert_eq!(d, Duration::from_millis(10));
            sleeps.set(sleeps.get() + 1);
            if sleeps.get() == 3 {
                *s.lock().unwrap() = Some(ITEM.to_owned());
            }
        };
        assert_eq!(copy_item_text(&sim, &clip, opts(), &sleep).unwrap(), ITEM);
        // Previous clipboard was empty, so restoring clears it again.
        assert_eq!(*state.lock().unwrap(), None);
    }

    #[test]
    fn times_out_when_nothing_arrives() {
        let state = Arc::new(Mutex::new(Some("keep me".to_owned())));
        let clip = stateful_clip(&state);
        let mut sim = MockInputSimulator::new();
        sim.expect_send_chord().returning(|_| Ok(()));
        let count = Cell::new(0u32);
        let err = copy_item_text(&sim, &clip, opts(), &|_| count.set(count.get() + 1)).unwrap_err();
        assert!(matches!(err, PlatformError::Timeout(d) if d == Duration::from_millis(100)), "{err:?}");
        assert_eq!(count.get(), 10);
        assert_eq!(state.lock().unwrap().as_deref(), Some("keep me"));
    }

    #[test]
    fn non_item_text_reports_no_item_text() {
        let state: State = Arc::new(Mutex::new(None));
        let clip = stateful_clip(&state);
        let mut sim = MockInputSimulator::new();
        let s = Arc::clone(&state);
        sim.expect_send_chord().returning(move |_| {
            *s.lock().unwrap() = Some("not an item".to_owned());
            Ok(())
        });
        let err = copy_item_text(&sim, &clip, opts(), &|_| {}).unwrap_err();
        assert!(matches!(err, PlatformError::NoItemText), "{err:?}");
    }

    #[test]
    fn zero_poll_interval_still_terminates() {
        let state: State = Arc::new(Mutex::new(None));
        let clip = stateful_clip(&state);
        let mut sim = MockInputSimulator::new();
        sim.expect_send_chord().returning(|_| Ok(()));
        let o = CopyOptions { poll_interval: Duration::ZERO, timeout: Duration::from_millis(3), ..opts() };
        assert!(matches!(copy_item_text(&sim, &clip, o, &|_| {}), Err(PlatformError::Timeout(_))));
    }

    #[test]
    fn simulator_error_is_returned_after_restoring() {
        let state = Arc::new(Mutex::new(Some("prev".to_owned())));
        let clip = stateful_clip(&state);
        let mut sim = MockInputSimulator::new();
        sim.expect_send_chord().returning(|_| Err(PlatformError::Unsupported("keystroke_injection")));
        let err = copy_item_text(&sim, &clip, opts(), &|_| panic!("must not poll")).unwrap_err();
        assert!(matches!(err, PlatformError::Unsupported(_)));
        assert_eq!(state.lock().unwrap().as_deref(), Some("prev"));
    }

    #[test]
    fn unreadable_clipboard_and_failing_restore_are_tolerated() {
        let mut clip = MockClipboard::new();
        let mut seq = mockall::Sequence::new();
        clip.expect_get_text().times(1).in_sequence(&mut seq).returning(|| Err(PlatformError::Backend("image".into())));
        clip.expect_clear().times(1).in_sequence(&mut seq).returning(|| Ok(()));
        clip.expect_get_text().times(1).in_sequence(&mut seq).returning(|| Err(PlatformError::Backend("busy".into())));
        clip.expect_get_text().times(1).in_sequence(&mut seq).returning(|| Ok(Some(ITEM.to_owned())));
        clip.expect_clear().times(1).in_sequence(&mut seq).returning(|| Err(PlatformError::Backend("gone".into())));
        let mut sim = MockInputSimulator::new();
        sim.expect_send_chord().returning(|_| Ok(()));
        assert_eq!(copy_item_text(&sim, &clip, opts(), &|_| {}).unwrap(), ITEM);
    }

    #[test]
    fn clear_failure_aborts_before_sending() {
        let mut clip = MockClipboard::new();
        clip.expect_get_text().returning(|| Ok(None));
        clip.expect_clear().returning(|| Err(PlatformError::Backend("no clipboard".into())));
        let sim = MockInputSimulator::new();
        assert!(matches!(copy_item_text(&sim, &clip, opts(), &|_| {}), Err(PlatformError::Backend(_))));
    }
}
