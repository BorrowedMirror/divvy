//! Last-resort keystroke injection through the `ydotool` CLI (uinput; needs `ydotoold`).
use std::path::{Path, PathBuf};
use std::process::Command;

use crate::{Chord, InputSimulator, KeyModifier, PlatformError};

#[derive(Debug, Clone)]
pub(crate) struct YdotoolSimulator {
    binary: PathBuf,
}

impl YdotoolSimulator {
    /// `Some` when `ydotool` is on `PATH`.
    pub(crate) fn detect() -> Option<Self> {
        find_in_path("ydotool", std::env::var_os("PATH")?.as_os_str()).map(|binary| Self { binary })
    }
}

pub(crate) fn find_in_path(name: &str, path: &std::ffi::OsStr) -> Option<PathBuf> {
    std::env::split_paths(path).map(|d| d.join(name)).find(|p| is_executable(p))
}

fn is_executable(p: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;
    p.metadata().is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0)
}

/// Linux evdev keycode (`input-event-codes.h`) for a chord key.
pub(crate) fn evdev_code(key: &str) -> Option<u16> {
    const LETTERS: &[u8; 26] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZ";
    const LETTER_CODES: [u16; 26] =
        [30, 48, 46, 32, 18, 33, 34, 35, 23, 36, 37, 38, 50, 49, 24, 25, 16, 19, 31, 20, 22, 47, 17, 45, 21, 44];
    if let Some(c) = key.strip_prefix("Key").filter(|c| c.len() == 1) {
        let idx = LETTERS.iter().position(|l| *l == c.as_bytes()[0])?;
        return Some(LETTER_CODES[idx]);
    }
    if let Some(d) = key.strip_prefix("Digit").and_then(|d| d.parse::<u16>().ok()).filter(|d| *d <= 9) {
        return Some(if d == 0 { 11 } else { d + 1 });
    }
    if let Some(n) = key.strip_prefix('F').and_then(|n| n.parse::<u16>().ok()) {
        return match n {
            1..=10 => Some(58 + n),
            11 => Some(87),
            12 => Some(88),
            _ => None,
        };
    }
    Some(match key {
        "Escape" => 1,
        "Tab" => 15,
        "Enter" => 28,
        "Space" => 57,
        _ => return None,
    })
}

fn modifier_code(m: KeyModifier) -> u16 {
    match m {
        KeyModifier::Ctrl => 29,
        KeyModifier::Shift => 42,
        KeyModifier::Alt => 56,
        KeyModifier::Meta => 125,
    }
}

/// `ydotool key` arguments: modifiers down, key down/up, modifiers up (reverse order).
pub(crate) fn key_args(chord: &Chord) -> Result<Vec<String>, PlatformError> {
    let key = evdev_code(&chord.key).ok_or_else(|| PlatformError::InvalidChord(chord.to_string()))?;
    let mods: Vec<u16> = chord.modifiers.iter().map(|m| modifier_code(*m)).collect();
    let mut args = vec!["key".to_owned()];
    args.extend(mods.iter().map(|c| format!("{c}:1")));
    args.push(format!("{key}:1"));
    args.push(format!("{key}:0"));
    args.extend(mods.iter().rev().map(|c| format!("{c}:0")));
    Ok(args)
}

impl InputSimulator for YdotoolSimulator {
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError> {
        let args = key_args(chord)?;
        let status = Command::new(&self.binary)
            .args(&args)
            .status()
            .map_err(|e| PlatformError::Backend(format!("ydotool: {e}")))?;
        if status.success() { Ok(()) } else { Err(PlatformError::Backend(format!("ydotool exited with {status}"))) }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn builds_key_sequence() {
        let args = key_args(&Chord::parse("Ctrl+Alt+C").unwrap()).unwrap();
        assert_eq!(args, ["key", "29:1", "56:1", "46:1", "46:0", "56:0", "29:0"]);
        assert!(key_args(&Chord::parse("ctrl+Pause").unwrap()).is_err());
    }

    #[test]
    fn evdev_codes() {
        assert_eq!(evdev_code("KeyA"), Some(30));
        assert_eq!(evdev_code("KeyZ"), Some(44));
        assert_eq!(evdev_code("Digit0"), Some(11));
        assert_eq!(evdev_code("Digit1"), Some(2));
        assert_eq!(evdev_code("F1"), Some(59));
        assert_eq!(evdev_code("F10"), Some(68));
        assert_eq!(evdev_code("F12"), Some(88));
        assert_eq!(evdev_code("F13"), None);
        assert_eq!(evdev_code("Space"), Some(57));
        assert_eq!(evdev_code("Keya"), None);
        assert_eq!(modifier_code(KeyModifier::Shift), 42);
        assert_eq!(modifier_code(KeyModifier::Meta), 125);
    }

    #[test]
    fn runs_a_fake_binary() {
        let dir = std::env::temp_dir().join(format!("kalandra-ydotool-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let ok = YdotoolSimulator { binary: PathBuf::from("/bin/true") };
        ok.send_chord(&Chord::parse("ctrl+c").unwrap()).unwrap();
        let fail = YdotoolSimulator { binary: PathBuf::from("/bin/false") };
        assert!(fail.send_chord(&Chord::parse("ctrl+c").unwrap()).is_err());
        let missing = YdotoolSimulator { binary: dir.join("nope") };
        assert!(missing.send_chord(&Chord::parse("ctrl+c").unwrap()).is_err());
        assert_eq!(find_in_path("true", std::ffi::OsStr::new("/nonexistent:/bin")), Some(PathBuf::from("/bin/true")));
        assert_eq!(find_in_path("kalandra-no-such-binary", std::ffi::OsStr::new("/bin")), None);
        let _ = YdotoolSimulator::detect();
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
