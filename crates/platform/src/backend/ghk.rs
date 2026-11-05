//! [`Chord`] -> `global_hotkey` mapping shared by the X11, Windows and macOS backends.
//!
//! Pure and OS-independent, so its unit tests run on every host. Chord keys are W3C
//! `KeyboardEvent.code` names, the same names [`super::hook`] reports for key events.
use std::str::FromStr;

use global_hotkey::hotkey::{Code, HotKey, Modifiers};

use crate::{Chord, KeyModifier, PlatformError};

/// Convert a chord whose key is a W3C `KeyboardEvent.code` name (`KeyD`, `F5`) into a hotkey.
pub(crate) fn to_hotkey(chord: &Chord) -> Result<HotKey, PlatformError> {
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
    fn meta_and_super_are_the_same_hotkey() {
        // `HotKey::new` folds META into SUPER; X11 used to build hotkeys from "super+..." strings.
        let hk = to_hotkey(&Chord::parse("Meta+D").unwrap()).unwrap();
        assert_eq!(hk, "super+KeyD".parse::<HotKey>().unwrap());
    }

    #[test]
    fn matches_global_hotkey_string_parser() {
        for (chord, s) in [("Ctrl+Alt+D", "ctrl+alt+KeyD"), ("Shift+F5", "shift+F5"), ("Ctrl+Space", "ctrl+Space")] {
            let ours = to_hotkey(&Chord::parse(chord).unwrap()).unwrap();
            let theirs: HotKey = s.parse().unwrap();
            assert_eq!(ours.id(), theirs.id(), "{chord}");
        }
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
