//! Per-OS native overlay styling applied to the Tauri `overlay` window.
//! Each OS file is owned by its platform workstream (D windows, E macos, F linux).
use rkl_platform::BackendKind;

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
    rkl_platform::detect(&rkl_platform::EnvProbe::from_env(), settings_override)
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

/// Bring `window` in front of the game window (`owner` = `WindowInfo::native_id`), optionally
/// taking keyboard focus. Call after `show()`.
pub fn bring_to_front(window: &tauri::WebviewWindow, owner: Option<u64>, focus: bool) -> Result<(), String> {
    let _ = window.set_always_on_top(true);
    #[cfg(target_os = "linux")]
    linux::raise_above(window, owner, focus)?;
    #[cfg(not(target_os = "linux"))]
    {
        let _ = owner;
        if focus {
            window.set_focus().map_err(|e| e.to_string())?;
        }
    }
    Ok(())
}

/// Return keyboard focus to the game window after the overlay closes (X11; elsewhere the OS
/// gives focus back to the previously active window by itself).
pub fn refocus_game(owner: Option<u64>) {
    #[cfg(all(target_os = "linux", feature = "native"))]
    if let Some(owner) = owner
        && let Err(e) = rkl_platform::backend::x11::activate_window(owner)
    {
        tracing::debug!("refocus game: {e}");
    }
    #[cfg(not(all(target_os = "linux", feature = "native")))]
    let _ = owner;
}

/// Corner radius of the price card (CSS px), matching `rounded-xl` in the web view.
pub const CARD_RADIUS: f64 = 12.0;

/// Shape a card-sized window as a rounded rectangle (`radius` 0 clears it), so compositor
/// shadows follow the rounded card. X11 only; elsewhere the OS handles window shadows.
pub fn round_window(window: &tauri::WebviewWindow, size: (u32, u32), radius: u32) {
    #[cfg(target_os = "linux")]
    if let Err(e) = linux::round_window(window, size, radius) {
        tracing::debug!("round window: {e}");
    }
    #[cfg(not(target_os = "linux"))]
    let _ = (window, size, radius);
}
