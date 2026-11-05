//! [`Chord`] -> `global_hotkey` mapping.
//!
//! Pure and OS-independent; the macOS backend needs the same mapping, so this would ideally live in
//! a shared `backend/ghk.rs` (owned by the platform-common workstream). It is private to the
//! Windows backend for now and also compiled by `tests/windows_logic.rs` via `#[path]`.
use std::str::FromStr;

use global_hotkey::hotkey::{Code, HotKey, Modifiers};

use crate::{Chord, KeyModifier, PlatformError};

/// Convert a chord whose key is a W3C `KeyboardEvent.code` name (`KeyD`, `F5`) into a hotkey.
pub fn to_hotkey(chord: &Chord) -> Result<HotKey, PlatformError> {
    let invalid = || PlatformError::InvalidChord(chord.to_string());
    let code = Code::from_str(&chord.key).map_err(|_| invalid())?;
    if is_modifier_or_unidentified(code) {
        return Err(invalid());
    }
    let mods = chord.modifiers.iter().fold(Modifiers::empty(), |acc, m| {
        acc | match m {
            KeyModifier::Ctrl => Modifiers::CONTROL,
            KeyModifier::Alt => Modifiers::ALT,
            KeyModifier::Shift => Modifiers::SHIFT,
            KeyModifier::Meta => Modifiers::SUPER,
        }
    });
    Ok(HotKey::new(Some(mods), code))
}

fn is_modifier_or_unidentified(code: Code) -> bool {
    matches!(
        code,
        Code::Unidentified
            | Code::ControlLeft
            | Code::ControlRight
            | Code::AltLeft
            | Code::AltRight
            | Code::ShiftLeft
            | Code::ShiftRight
            | Code::MetaLeft
            | Code::MetaRight
            | Code::Fn
            | Code::FnLock
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_letters_function_keys_and_modifiers() {
        let hk = to_hotkey(&Chord::parse("Ctrl+D").unwrap()).unwrap();
        assert_eq!(hk.key, Code::KeyD);
        assert_eq!(hk.mods, Modifiers::CONTROL);

        let hk = to_hotkey(&Chord::parse("Ctrl+Alt+Shift+Meta+F5").unwrap()).unwrap();
        assert_eq!(hk.key, Code::F5);
        assert_eq!(hk.mods, Modifiers::CONTROL | Modifiers::ALT | Modifiers::SHIFT | Modifiers::SUPER);

        let hk = to_hotkey(&Chord::parse("Alt+1").unwrap()).unwrap();
        assert_eq!(hk.key, Code::Digit1);

        assert_eq!(to_hotkey(&Chord::new(&[], "Space")).unwrap().key, Code::Space);
    }

    #[test]
    fn same_chord_same_id() {
        let a = to_hotkey(&Chord::parse("Ctrl+D").unwrap()).unwrap();
        let b = to_hotkey(&Chord::parse("ctrl+d").unwrap()).unwrap();
        assert_eq!(a.id(), b.id());
    }

    #[test]
    fn rejects_unknown_and_modifier_keys() {
        for key in ["NotAKey", "Unidentified", "ControlLeft", "ShiftRight", "d"] {
            let err = to_hotkey(&Chord::new(&[KeyModifier::Ctrl], key)).unwrap_err();
            assert!(matches!(err, PlatformError::InvalidChord(_)), "{key}: {err:?}");
        }
    }
}
