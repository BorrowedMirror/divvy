//! Linux overlay styling for the Tauri (GTK) `overlay` window.
//!
//! - X11 / XWayland (default): GTK must use its X11 backend so window positions are global and
//!   the EWMH always-on-top/skip-taskbar hints work. [`pre_init`] forces `GDK_BACKEND=x11`
//!   before GTK initialises; [`prepare`] applies the hints through Tauri.
//! - Wayland-native (no `DISPLAY`, or `RKL_BACKEND=wayland`): the window becomes a
//!   wlr-layer-shell surface on the `OVERLAY` layer via gtk-layer-shell (not GNOME: mutter has
//!   no layer-shell). gtk-layer-shell must be initialised *before* the GTK window is realised.
//!   Tauri creates the `overlay` window hidden, and GTK only realises a window when it is first
//!   shown, so calling [`prepare`] from `setup()` before the first `show()` is enough. If the
//!   window was already realised we hide + unrealise it first (best effort: the webview is
//!   re-realised with the new layer surface on the next `show()`).
use std::sync::OnceLock;

use rkl_platform::{BackendKind, EnvProbe, detect};

static BACKEND: OnceLock<BackendKind> = OnceLock::new();

/// Call first thing in `run()`, before `tauri::Builder` (i.e. before GTK initialises).
/// Returns the backend the process will use.
pub fn pre_init() -> BackendKind {
    pre_init_with(None)
}

/// [`pre_init`] honouring the `platform.backend` settings override.
pub fn pre_init_with(settings_override: Option<BackendKind>) -> BackendKind {
    let kind = *BACKEND.get_or_init(|| detect(&EnvProbe::from_env(), settings_override));
    let gdk = gdk_backend_for(kind);
    #[allow(unsafe_code)]
    // SAFETY: `pre_init` runs at the top of `run()`, before Tauri/GTK/tokio start any thread,
    // so nothing can read the environment concurrently.
    unsafe {
        std::env::set_var("GDK_BACKEND", gdk);
    }
    kind
}

/// GDK backend matching the platform backend.
pub fn gdk_backend_for(kind: BackendKind) -> &'static str {
    if kind == BackendKind::Wayland { "wayland" } else { "x11" }
}

fn backend() -> BackendKind {
    *BACKEND.get_or_init(|| detect(&EnvProbe::from_env(), None))
}

/// Apply native styles to the overlay window. Call once from `setup()`, before first show.
pub fn prepare(window: &tauri::WebviewWindow) -> Result<(), String> {
    window.set_always_on_top(true).map_err(|e| e.to_string())?;
    window.set_skip_taskbar(true).map_err(|e| e.to_string())?;
    window.set_visible_on_all_workspaces(true).map_err(|e| e.to_string())?;
    if backend() == BackendKind::Wayland && !EnvProbe::from_env().is_gnome() {
        init_layer_shell(window)?;
    }
    Ok(())
}

/// Let clicks pass through the overlay (e.g. while it only shows a passive price badge).
pub fn set_click_through(window: &tauri::WebviewWindow, enabled: bool) -> Result<(), String> {
    window.set_ignore_cursor_events(enabled).map_err(|e| e.to_string())
}

/// Wayland-native only: place the layer surface against the right (or left) output edge.
/// `set_position` has no effect on layer surfaces; on X11 use `set_position` instead.
pub fn anchor_layer(window: &tauri::WebviewWindow, right: bool, top: i32, side: i32) -> Result<(), String> {
    use gtk_layer_shell::{Edge, LayerShell};
    let gtk_window = window.gtk_window().map_err(|e| e.to_string())?;
    if !gtk_window.is_layer_window() {
        return Err("overlay is not a layer-shell surface".into());
    }
    gtk_window.set_anchor(Edge::Top, true);
    gtk_window.set_anchor(Edge::Right, right);
    gtk_window.set_anchor(Edge::Left, !right);
    gtk_window.set_layer_shell_margin(Edge::Top, top);
    gtk_window.set_layer_shell_margin(if right { Edge::Right } else { Edge::Left }, side);
    Ok(())
}

fn init_layer_shell(window: &tauri::WebviewWindow) -> Result<(), String> {
    use gtk::prelude::*;
    use gtk_layer_shell::{Edge, KeyboardMode, Layer, LayerShell};

    if !gtk_layer_shell::is_supported() {
        return Err("compositor does not implement wlr-layer-shell; overlay will be a normal window".into());
    }
    let gtk_window = window.gtk_window().map_err(|e| e.to_string())?;
    if gtk_window.is_layer_window() {
        return Ok(());
    }
    if gtk_window.is_realized() {
        tracing::warn!("overlay window already realised; unrealising to attach layer-shell");
        gtk_window.hide();
        gtk_window.unrealize();
    }
    gtk_window.init_layer_shell();
    gtk_window.set_layer(Layer::Overlay);
    gtk_window.set_namespace("rkl-overlay");
    // Keyboard only when the user clicks into the overlay (filter editing); never steal it.
    gtk_window.set_keyboard_mode(KeyboardMode::OnDemand);
    gtk_window.set_exclusive_zone(-1);
    gtk_window.set_anchor(Edge::Top, true);
    gtk_window.set_anchor(Edge::Right, true);
    gtk_window.set_layer_shell_margin(Edge::Top, 48);
    gtk_window.set_layer_shell_margin(Edge::Right, 24);
    Ok(())
}

/// X11: keep `window` above the game window (`owner` = its X id) even when the game is a
/// focused fullscreen client (openbox and other WMs stack those above "always on top"), and
/// optionally give it keyboard focus. No-op on the Wayland-native backend (layer-shell already
/// sits in the overlay layer).
pub fn raise_above(window: &tauri::WebviewWindow, owner: Option<u64>, focus: bool) -> Result<(), String> {
    use raw_window_handle::{HasDisplayHandle, HasWindowHandle};
    if backend() == BackendKind::Wayland {
        return Ok(());
    }
    let handle = window.window_handle().map_err(|e| format!("window handle: {e}"))?.as_raw();
    let display = window.display_handle().map_err(|e| format!("display handle: {e}"))?.as_raw();
    let styler = rkl_platform::backend::x11::overlay(handle, display).map_err(|e| e.to_string())?;
    // No compositor drop shadow around the transparent window edge (picom draws one by default).
    let id = match handle {
        raw_window_handle::RawWindowHandle::Xlib(h) => Some(h.window),
        raw_window_handle::RawWindowHandle::Xcb(h) => Some(u64::from(h.window.get())),
        _ => None,
    };
    if let Some(id) = id
        && let Err(e) = rkl_platform::backend::x11::disable_compositor_shadow(id)
    {
        tracing::debug!("disable compositor shadow: {e}");
    }
    styler.raise_above(owner).map_err(|e| e.to_string())?;
    if focus {
        styler.activate().map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn gdk_backend_matches_platform_backend() {
        assert_eq!(gdk_backend_for(BackendKind::X11), "x11");
        assert_eq!(gdk_backend_for(BackendKind::Fake), "x11");
        assert_eq!(gdk_backend_for(BackendKind::Wayland), "wayland");
    }
}
