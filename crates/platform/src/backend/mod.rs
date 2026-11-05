//! Concrete backends. Each submodule exposes `pub fn create() -> Result<crate::Platform, PlatformError>`
//! and an overlay styler `pub fn overlay(handle: raw_window_handle::RawWindowHandle) -> Box<dyn OverlayWindow>`.
use std::sync::mpsc::Receiver;

use crate::{
    BackendKind, Chord, Clipboard, HotkeyAction, HotkeyEvent, HotkeyHost, InputEvent, InputListener, InputSimulator,
    Platform, PlatformError, WindowEvent, WindowInfo, WindowTracker,
};

// Pure chord planning shared by every backend (unit-tested on all OSes).
#[cfg_attr(not(feature = "native"), allow(dead_code))]
pub(crate) mod chord_plan;
// Shared by the x11, windows and macos backends: the single process-wide rdev hook and the
// Chord -> global-hotkey mapping.
#[cfg(feature = "native")]
pub(crate) mod ghk;
#[cfg(feature = "native")]
pub(crate) mod hook;
#[cfg(all(target_os = "macos", feature = "native"))]
pub mod macos;
#[cfg(all(target_os = "linux", feature = "native"))]
pub mod wayland;
#[cfg(all(target_os = "windows", feature = "native"))]
pub mod windows;
#[cfg(all(target_os = "linux", feature = "native"))]
pub mod x11;

/// Instantiate the backend for `kind`.
pub fn create(kind: BackendKind) -> Result<Platform, PlatformError> {
    match kind {
        #[cfg(all(target_os = "linux", feature = "native"))]
        BackendKind::X11 => x11::create(),
        #[cfg(all(target_os = "linux", feature = "native"))]
        BackendKind::Wayland => wayland::create(),
        #[cfg(all(target_os = "macos", feature = "native"))]
        BackendKind::Macos => macos::create(),
        #[cfg(all(target_os = "windows", feature = "native"))]
        BackendKind::Windows => windows::create(),
        BackendKind::Fake => Err(PlatformError::Unsupported("fake backend lives in kalandra-testkit")),
        #[allow(unreachable_patterns)]
        _ => Err(PlatformError::Unsupported("backend not compiled for this OS")),
    }
}

/// Stand-in for a capability the running backend cannot provide. Every call returns
/// `PlatformError::Unsupported(capability)` (the matching `Capabilities` flag is `false`).
#[derive(Debug, Clone, Copy)]
pub struct Unsupported(pub &'static str);

impl WindowTracker for Unsupported {
    fn game_window(&self) -> Option<WindowInfo> {
        None
    }
    /// Without tracking we cannot tell; assume focused so hotkeys still work.
    fn is_game_focused(&self) -> bool {
        true
    }
    fn subscribe(&self) -> Result<Receiver<WindowEvent>, PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
}

impl HotkeyHost for Unsupported {
    fn register(&self, _id: HotkeyAction, _chord: &Chord) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
    fn unregister(&self, _id: HotkeyAction) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
    fn unregister_all(&self) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
}

impl InputListener for Unsupported {
    fn start(&self) -> Result<Receiver<InputEvent>, PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
}

impl InputSimulator for Unsupported {
    fn send_chord(&self, _chord: &Chord) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
}

impl Clipboard for Unsupported {
    fn get_text(&self) -> Result<Option<String>, PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
    fn set_text(&self, _text: &str) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
    fn clear(&self) -> Result<(), PlatformError> {
        Err(PlatformError::Unsupported(self.0))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unsupported_reports_capability() {
        let u = Unsupported("x");
        let is_x = |e: PlatformError| matches!(e, PlatformError::Unsupported("x"));
        assert!(WindowTracker::game_window(&u).is_none());
        assert!(u.is_game_focused());
        assert!(is_x(u.subscribe().unwrap_err()));
        let chord = Chord::parse("ctrl+d").unwrap();
        assert!(is_x(u.register(HotkeyAction::PriceCheck, &chord).unwrap_err()));
        assert!(is_x(u.unregister(HotkeyAction::PriceCheck).unwrap_err()));
        assert!(is_x(u.unregister_all().unwrap_err()));
        assert!(is_x(u.events().unwrap_err()));
        assert!(is_x(u.start().unwrap_err()));
        assert!(is_x(u.send_chord(&chord).unwrap_err()));
        assert!(is_x(u.get_text().unwrap_err()));
        assert!(is_x(u.set_text("a").unwrap_err()));
        assert!(is_x(u.clear().unwrap_err()));
    }

    #[test]
    fn create_fake_is_unsupported_here() {
        assert!(matches!(create(BackendKind::Fake), Err(PlatformError::Unsupported(_))));
        assert!(matches!(create(BackendKind::Macos), Err(PlatformError::Unsupported(_))));
    }
}
