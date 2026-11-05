//! Placeholder; workstream F owns this file.

/// Set `GDK_BACKEND=x11` before GTK initialises.
#[allow(unsafe_code)]
pub fn force_x11_backend() {
    // SAFETY: called from `run()` on the main thread before Tauri, GTK or any worker threads
    // start, so no other thread can be reading the environment concurrently.
    unsafe { std::env::set_var("GDK_BACKEND", "x11") };
}

pub fn prepare(window: &tauri::WebviewWindow) -> Result<(), String> {
    let _ = window;
    Ok(())
}
