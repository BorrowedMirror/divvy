//! Per-OS native overlay styling applied to the Tauri `overlay` window.
//! Each OS file is owned by its platform workstream (D windows, E macos, F linux).
use kalandra_platform::BackendKind;

#[cfg(target_os = "linux")]
pub mod linux;
#[cfg(target_os = "macos")]
pub mod macos;
#[cfg(target_os = "windows")]
pub mod windows;

/// Process-level setup that must happen before Tauri/GTK initialise; returns the backend to use.
/// On Linux this also pins `GDK_BACKEND` (x11 for X11/XWayland, wayland for Wayland-native) so
/// the overlay can use `_NET_WM_STATE_ABOVE` + absolute positioning or layer-shell respectively.
pub fn pre_init(settings_override: Option<BackendKind>) -> BackendKind {
    #[cfg(target_os = "linux")]
    return linux::pre_init_with(settings_override);
    #[allow(unreachable_code)]
    kalandra_platform::detect(&kalandra_platform::EnvProbe::from_env(), settings_override)
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
