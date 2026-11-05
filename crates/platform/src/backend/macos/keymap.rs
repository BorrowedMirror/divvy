//! W3C `KeyboardEvent.code` <-> macOS virtual keycode (`kVK_*`, Carbon `Events.h`).
//!
//! Pure; included by `crates/platform/tests/macos_model.rs` so it is tested on every OS.
//! Virtual keycodes are layout-independent (they name physical positions), exactly like W3C codes.

/// `(W3C code, kVK_* value)`.
const TABLE: &[(&str, u16)] = &[
    ("KeyA", 0x00),
    ("KeyS", 0x01),
    ("KeyD", 0x02),
    ("KeyF", 0x03),
    ("KeyH", 0x04),
    ("KeyG", 0x05),
    ("KeyZ", 0x06),
    ("KeyX", 0x07),
    ("KeyC", 0x08),
    ("KeyV", 0x09),
    ("IntlBackslash", 0x0A),
    ("KeyB", 0x0B),
    ("KeyQ", 0x0C),
    ("KeyW", 0x0D),
    ("KeyE", 0x0E),
    ("KeyR", 0x0F),
    ("KeyY", 0x10),
    ("KeyT", 0x11),
    ("Digit1", 0x12),
    ("Digit2", 0x13),
    ("Digit3", 0x14),
    ("Digit4", 0x15),
    ("Digit6", 0x16),
    ("Digit5", 0x17),
    ("Equal", 0x18),
    ("Digit9", 0x19),
    ("Digit7", 0x1A),
    ("Minus", 0x1B),
    ("Digit8", 0x1C),
    ("Digit0", 0x1D),
    ("BracketRight", 0x1E),
    ("KeyO", 0x1F),
    ("KeyU", 0x20),
    ("BracketLeft", 0x21),
    ("KeyI", 0x22),
    ("KeyP", 0x23),
    ("Enter", 0x24),
    ("KeyL", 0x25),
    ("KeyJ", 0x26),
    ("Quote", 0x27),
    ("KeyK", 0x28),
    ("Semicolon", 0x29),
    ("Backslash", 0x2A),
    ("Comma", 0x2B),
    ("Slash", 0x2C),
    ("KeyN", 0x2D),
    ("KeyM", 0x2E),
    ("Period", 0x2F),
    ("Tab", 0x30),
    ("Space", 0x31),
    ("Backquote", 0x32),
    ("Backspace", 0x33),
    ("Escape", 0x35),
    ("MetaRight", 0x36),
    ("MetaLeft", 0x37),
    ("ShiftLeft", 0x38),
    ("CapsLock", 0x39),
    ("AltLeft", 0x3A),
    ("ControlLeft", 0x3B),
    ("ShiftRight", 0x3C),
    ("AltRight", 0x3D),
    ("ControlRight", 0x3E),
    ("Fn", 0x3F),
    ("F17", 0x40),
    ("NumpadDecimal", 0x41),
    ("NumpadMultiply", 0x43),
    ("NumpadAdd", 0x45),
    ("NumLock", 0x47),
    ("NumpadDivide", 0x4B),
    ("NumpadEnter", 0x4C),
    ("NumpadSubtract", 0x4E),
    ("F18", 0x4F),
    ("F19", 0x50),
    ("NumpadEqual", 0x51),
    ("Numpad0", 0x52),
    ("Numpad1", 0x53),
    ("Numpad2", 0x54),
    ("Numpad3", 0x55),
    ("Numpad4", 0x56),
    ("Numpad5", 0x57),
    ("Numpad6", 0x58),
    ("Numpad7", 0x59),
    ("F20", 0x5A),
    ("Numpad8", 0x5B),
    ("Numpad9", 0x5C),
    ("F5", 0x60),
    ("F6", 0x61),
    ("F7", 0x62),
    ("F3", 0x63),
    ("F8", 0x64),
    ("F9", 0x65),
    ("F11", 0x67),
    ("F13", 0x69),
    ("F16", 0x6A),
    ("F14", 0x6B),
    ("F10", 0x6D),
    ("F12", 0x6F),
    ("F15", 0x71),
    // kVK_Help sits where Insert is on PC keyboards.
    ("Insert", 0x72),
    ("Home", 0x73),
    ("PageUp", 0x74),
    ("Delete", 0x75),
    ("F4", 0x76),
    ("End", 0x77),
    ("F2", 0x78),
    ("PageDown", 0x79),
    ("F1", 0x7A),
    ("ArrowLeft", 0x7B),
    ("ArrowRight", 0x7C),
    ("ArrowDown", 0x7D),
    ("ArrowUp", 0x7E),
];

/// Virtual keycode for a W3C code, e.g. `KeyC` -> `0x08`.
pub fn mac_keycode(code: &str) -> Option<u16> {
    TABLE.iter().find(|(c, _)| *c == code).map(|&(_, k)| k)
}

/// W3C code for a virtual keycode; `None` for unmapped codes.
pub fn code_for_keycode(keycode: u16) -> Option<&'static str> {
    TABLE.iter().find(|&&(_, k)| k == keycode).map(|&(c, _)| c)
}

/// W3C code for a keycode, or `Unidentified` (the W3C name for unknown keys).
pub fn code_name(keycode: u16) -> &'static str {
    code_for_keycode(keycode).unwrap_or("Unidentified")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_known_keys() {
        assert_eq!(mac_keycode("KeyC"), Some(0x08));
        assert_eq!(mac_keycode("KeyD"), Some(0x02));
        assert_eq!(mac_keycode("F5"), Some(0x60));
        assert_eq!(mac_keycode("Escape"), Some(0x35));
        assert_eq!(mac_keycode("Nope"), None);
        assert_eq!(code_for_keycode(0x08), Some("KeyC"));
        assert_eq!(code_for_keycode(0x42), None);
        assert_eq!(code_name(0x35), "Escape");
        assert_eq!(code_name(0xFFFF), "Unidentified");
    }

    #[test]
    fn table_is_a_bijection() {
        for &(code, key) in TABLE {
            assert_eq!(mac_keycode(code), Some(key), "{code}");
            assert_eq!(code_for_keycode(key), Some(code), "{key:#x}");
        }
    }

    #[test]
    fn covers_every_letter_digit_and_function_key() {
        for c in 'A'..='Z' {
            assert!(mac_keycode(&format!("Key{c}")).is_some(), "Key{c}");
        }
        for d in 0..=9 {
            assert!(mac_keycode(&format!("Digit{d}")).is_some(), "Digit{d}");
            assert!(mac_keycode(&format!("Numpad{d}")).is_some(), "Numpad{d}");
        }
        for f in 1..=20 {
            assert!(mac_keycode(&format!("F{f}")).is_some(), "F{f}");
        }
    }

    #[test]
    fn chord_parse_output_is_mappable() {
        // `Chord::parse` emits KeyX / DigitN; those must all resolve.
        let chord = crate::types::Chord::parse("ctrl+alt+c").expect("chord");
        assert_eq!(mac_keycode(&chord.key), Some(0x08));
    }
}
