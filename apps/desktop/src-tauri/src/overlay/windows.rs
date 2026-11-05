//! Windows overlay styling: hands the Tauri window's HWND to the platform backend, which applies
//! `WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW | WS_EX_TOPMOST`. No unsafe code needed here.

#[cfg(feature = "native")]
pub fn prepare(window: &tauri::WebviewWindow) -> Result<(), String> {
    use rkl_platform::backend::windows::{
        overlay,
        raw_window_handle::{HasDisplayHandle, HasWindowHandle},
    };

    let handle = window.window_handle().map_err(|e| format!("overlay window handle: {e}"))?.as_raw();
    let display = window.display_handle().map_err(|e| format!("overlay display handle: {e}"))?.as_raw();
    overlay(handle, display).and_then(|o| o.prepare()).map_err(|e| e.to_string())
}

/// Without native backends there is nothing to style.
#[cfg(not(feature = "native"))]
pub fn prepare(window: &tauri::WebviewWindow) -> Result<(), String> {
    let _ = window;
    Ok(())
}
