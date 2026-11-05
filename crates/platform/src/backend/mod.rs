//! Concrete backends. Each submodule exposes `pub fn create() -> Result<crate::Platform, PlatformError>`
//! and an overlay styler `pub fn overlay(handle: raw_window_handle::RawWindowHandle) -> Box<dyn OverlayWindow>`.
use crate::{BackendKind, Platform, PlatformError};

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
