//! Windows backend (workstream D).
//!
//! - `tracker`: `SetWinEventHook` (foreground + location change) on a dedicated message-loop
//!   thread, plus a 1 s rescan timer for the game window appearing/disappearing.
//! - `hotkeys`: `global-hotkey` manager owned by its own message-loop thread (its hidden
//!   `WM_HOTKEY` window must be pumped by the thread that created it).
//! - `input`: the shared `backend::hook` passive hook and `enigo` chord injection.
//! - `clipboard`: `arboard`.
//! - `overlay`: ex-style based no-activate / topmost / click-through styling of an HWND.
//!
//! Every `unsafe` block carries a `// SAFETY:` comment.
mod clipboard;
mod hotkeys;
mod input;
mod logic;
mod overlay;
mod tracker;

/// Re-exported so the desktop crate can obtain a [`raw_window_handle::RawWindowHandle`] from its
/// Tauri window (`HasWindowHandle`) and hand it to [`overlay()`] without depending on, or writing
/// unsafe code for, Win32 itself.
pub use raw_window_handle;

use raw_window_handle::{RawDisplayHandle, RawWindowHandle};

use crate::{BackendKind, OverlayWindow, Platform, PlatformError};

/// Build the Windows platform bundle. Only hotkey setup can fail at runtime (it needs a hidden
/// message window); that degrades to an unavailable host reported via `Capabilities`.
pub fn create() -> Result<Platform, PlatformError> {
    let (hotkeys, hotkeys_ok): (Box<dyn crate::HotkeyHost>, bool) = match hotkeys::Win32HotkeyHost::spawn() {
        Ok(host) => (Box::new(host), true),
        Err(err) => {
            tracing::warn!(%err, "global hotkeys unavailable");
            (Box::new(hotkeys::UnavailableHotkeys), false)
        }
    };
    Ok(Platform {
        kind: BackendKind::Windows,
        capabilities: logic::capabilities(hotkeys_ok),
        window_tracker: Box::new(tracker::Win32WindowTracker::default()),
        hotkeys,
        input_listener: Box::new(input::RdevInputListener),
        input_simulator: Box::new(input::EnigoSimulator),
        clipboard: Box::new(clipboard::ArboardClipboard),
    })
}

/// Native styler for the overlay window. Only Win32 handles are accepted; the display handle is
/// irrelevant on Windows.
pub fn overlay(handle: RawWindowHandle, display: RawDisplayHandle) -> Result<Box<dyn OverlayWindow>, PlatformError> {
    let _ = display;
    match handle {
        RawWindowHandle::Win32(h) => Ok(Box::new(overlay::Win32Overlay::new(h.hwnd.get()))),
        _ => Err(PlatformError::Unsupported("overlay: expected a Win32 window handle")),
    }
}

#[cfg(test)]
mod tests {
    use std::num::NonZeroIsize;

    use raw_window_handle::{Win32WindowHandle, WindowsDisplayHandle, XlibWindowHandle};

    use super::*;

    #[test]
    fn overlay_requires_win32_handle() {
        let display = RawDisplayHandle::Windows(WindowsDisplayHandle::new());
        let xlib = RawWindowHandle::Xlib(XlibWindowHandle::new(1));
        assert!(matches!(overlay(xlib, display), Err(PlatformError::Unsupported(_))));
        let win = RawWindowHandle::Win32(Win32WindowHandle::new(NonZeroIsize::new(0x1234).unwrap()));
        assert!(overlay(win, display).is_ok());
    }
}
