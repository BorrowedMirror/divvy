//! kalandra-platform: OS integration behind traits.
//!
//! Rules:
//! - Exactly one global input hook per process. Hook callbacks only push into a channel.
//! - Missing permissions are reported via [`Capabilities`], never by panicking or spinning.
//! - Backends live under [`backend`]; [`detect()`] chooses one at runtime.
pub mod backend;
pub mod copy;
pub mod detect;
pub mod error;
pub mod types;

use std::sync::mpsc::Receiver;

pub use detect::{BackendKind, EnvProbe, detect};
pub use error::PlatformError;
pub use types::*;

/// Tracks the PoE2 game window.
#[cfg_attr(test, mockall::automock)]
pub trait WindowTracker: Send + Sync {
    /// Current game window if found (title `Path of Exile 2`).
    fn game_window(&self) -> Option<WindowInfo>;
    /// Whether the game window is focused right now.
    fn is_game_focused(&self) -> bool;
    /// Start emitting window events; may be called once.
    fn subscribe(&self) -> Result<Receiver<WindowEvent>, PlatformError>;
}

/// Registers global hotkeys.
#[cfg_attr(test, mockall::automock)]
pub trait HotkeyHost: Send + Sync {
    fn register(&self, id: HotkeyAction, chord: &Chord) -> Result<(), PlatformError>;
    fn unregister(&self, id: HotkeyAction) -> Result<(), PlatformError>;
    fn unregister_all(&self) -> Result<(), PlatformError>;
    /// Stream of fired hotkeys; may be called once.
    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError>;
}

/// Passive keyboard/mouse hook (e.g. to detect Ctrl+scroll, Esc to close).
#[cfg_attr(test, mockall::automock)]
pub trait InputListener: Send + Sync {
    /// Start the single global hook. Second call returns `PlatformError::AlreadyStarted`.
    fn start(&self) -> Result<Receiver<InputEvent>, PlatformError>;
}

/// Synthesises the copy keystroke into the game.
#[cfg_attr(test, mockall::automock)]
pub trait InputSimulator: Send + Sync {
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError>;
}

/// System clipboard (text only).
#[cfg_attr(test, mockall::automock)]
pub trait Clipboard: Send + Sync {
    fn get_text(&self) -> Result<Option<String>, PlatformError>;
    fn set_text(&self, text: &str) -> Result<(), PlatformError>;
    fn clear(&self) -> Result<(), PlatformError>;
}

/// Native styling for the overlay window. Implemented over a raw window handle by each backend;
/// the desktop app owns the actual Tauri window and calls these.
#[cfg_attr(test, mockall::automock)]
pub trait OverlayWindow: Send + Sync {
    /// Apply one-time native styles (no-activate, always-on-top, all-spaces, layer-shell...).
    fn prepare(&self) -> Result<(), PlatformError>;
    fn show_at(&self, rect: Rect) -> Result<(), PlatformError>;
    fn hide(&self) -> Result<(), PlatformError>;
    fn set_click_through(&self, enabled: bool) -> Result<(), PlatformError>;
}

/// Everything a backend provides, plus what it can actually do here.
pub struct Platform {
    pub kind: BackendKind,
    pub capabilities: Capabilities,
    pub window_tracker: Box<dyn WindowTracker>,
    pub hotkeys: Box<dyn HotkeyHost>,
    pub input_listener: Box<dyn InputListener>,
    pub input_simulator: Box<dyn InputSimulator>,
    pub clipboard: Box<dyn Clipboard>,
}

impl std::fmt::Debug for Platform {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Platform")
            .field("kind", &self.kind)
            .field("capabilities", &self.capabilities)
            .finish_non_exhaustive()
    }
}
