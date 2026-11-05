//! Per-OS native overlay styling applied to the Tauri `overlay` window.
//! Each OS file is owned by its platform workstream (D windows, E macos, F linux).
use kalandra_platform::BackendKind;

#[cfg(target_os = "linux")]
pub mod linux;
#[cfg(target_os = "macos")]
pub mod macos;
#[cfg(target_os = "windows")]
pub mod windows;

/// Process-level setup that must happen before Tauri/GTK initialise.
/// On Linux with an X11/XWayland backend we force `GDK_BACKEND=x11` so the overlay can use
/// `_NET_WM_STATE_ABOVE` and absolute positioning (impossible for Wayland-native toplevels).
pub fn pre_init(backend: BackendKind) {
    #[cfg(target_os = "linux")]
    if backend == BackendKind::X11 && std::env::var_os("GDK_BACKEND").is_none() {
        // SAFETY-free: called on the main thread before any other thread is spawned.
        // `set_var` is unsafe in edition 2024; desktop denies unsafe, so pass the request to the
        // linux module which owns the justification.
        linux::force_x11_backend();
    }
    let _ = backend;
}

/// Apply native styles to the overlay window. Called once at startup.
pub fn prepare(window: &tauri::WebviewWindow) -> Result<(), String> {
    #[cfg(target_os = "linux")]
    return linux::prepare(window);
    #[cfg(target_os = "macos")]
    return macos::prepare(window);
    #[cfg(target_os = "windows")]
    return windows::prepare(window);
    #[allow(unreachable_code)]
    {
        let _ = window;
        Ok(())
    }
}
