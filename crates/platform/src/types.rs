use serde::{Deserialize, Serialize};

/// Window title we track.
pub const GAME_WINDOW_TITLE: &str = "Path of Exile 2";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize, specta::Type)]
pub struct Rect {
    pub x: i32,
    pub y: i32,
    pub width: u32,
    pub height: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct WindowInfo {
    pub title: String,
    pub rect: Rect,
    pub focused: bool,
    /// OS-specific id (HWND, CGWindowID, X11 window id) as u64.
    pub native_id: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum WindowEvent {
    Found(WindowInfo),
    Moved { rect: Rect },
    FocusChanged { focused: bool },
    Lost,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum KeyModifier {
    Ctrl,
    Alt,
    Shift,
    Meta,
}

/// A key chord such as `Ctrl+Alt+D`. `key` uses W3C `KeyboardEvent.code` names (`KeyD`, `F5`).
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
pub struct Chord {
    pub modifiers: Vec<KeyModifier>,
    pub key: String,
}

impl Chord {
    pub fn new(modifiers: &[KeyModifier], key: &str) -> Self {
        Self { modifiers: modifiers.to_vec(), key: key.to_owned() }
    }

    /// Parse `Ctrl+Alt+D` / `ctrl+shift+F5` style strings.
    pub fn parse(s: &str) -> Result<Self, crate::PlatformError> {
        let mut modifiers = Vec::new();
        let mut key = None;
        for part in s.split('+').map(str::trim).filter(|p| !p.is_empty()) {
            match part.to_ascii_lowercase().as_str() {
                "ctrl" | "control" => modifiers.push(KeyModifier::Ctrl),
                "alt" | "option" => modifiers.push(KeyModifier::Alt),
                "shift" => modifiers.push(KeyModifier::Shift),
                "meta" | "cmd" | "super" | "win" => modifiers.push(KeyModifier::Meta),
                other => {
                    if key.is_some() {
                        return Err(crate::PlatformError::InvalidChord(s.to_owned()));
                    }
                    key = Some(match other.len() {
                        1 if other.chars().all(|c| c.is_ascii_alphabetic()) => {
                            format!("Key{}", other.to_ascii_uppercase())
                        }
                        1 if other.chars().all(|c| c.is_ascii_digit()) => format!("Digit{other}"),
                        _ => part.to_owned(),
                    });
                }
            }
        }
        key.map(|key| Self { modifiers, key }).ok_or_else(|| crate::PlatformError::InvalidChord(s.to_owned()))
    }
}

impl std::fmt::Display for Chord {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        for m in &self.modifiers {
            let s = match m {
                KeyModifier::Ctrl => "Ctrl",
                KeyModifier::Alt => "Alt",
                KeyModifier::Shift => "Shift",
                KeyModifier::Meta => "Meta",
            };
            write!(f, "{s}+")?;
        }
        let k = self.key.strip_prefix("Key").or_else(|| self.key.strip_prefix("Digit")).unwrap_or(&self.key);
        f.write_str(k)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum HotkeyAction {
    PriceCheck,
    PriceCheckPinned,
    OpenSettings,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct HotkeyEvent {
    pub action: HotkeyAction,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum InputEvent {
    KeyDown { key: String },
    KeyUp { key: String },
    MouseMove { x: i32, y: i32 },
    Wheel { delta: i32 },
}

/// Per-capability support report shown on the diagnostics page.
#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize, specta::Type)]
pub struct Capabilities {
    pub window_tracking: bool,
    pub overlay_always_on_top: bool,
    pub overlay_click_through: bool,
    pub global_hotkeys: bool,
    pub passive_hook: bool,
    pub keystroke_injection: bool,
    pub clipboard: bool,
    /// Permissions still missing (macOS: `accessibility`, `screen_recording`).
    pub missing_permissions: Vec<String>,
    /// Human-readable notes, e.g. "Wayland-native: overlay anchored to output".
    pub notes: Vec<String>,
}
