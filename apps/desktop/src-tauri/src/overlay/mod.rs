//! Per-OS native overlay styling applied to the Tauri `overlay` window.
//! Each OS file is owned by its platform workstream (D windows, E macos, F linux).
#[cfg(target_os = "linux")]
pub mod linux;
#[cfg(target_os = "macos")]
pub mod macos;
#[cfg(target_os = "windows")]
pub mod windows;

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
