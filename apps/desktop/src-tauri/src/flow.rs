//! Orchestration glue between platform, core, trade and the Tauri windows.
use std::sync::{Arc, atomic::Ordering};

use kalandra_config::Settings;
use kalandra_core::{ParsedItem, catalog::Catalog, query::FilterSet};
use kalandra_platform::{
    Chord, HotkeyAction, KeyModifier,
    copy::{CopyOptions, copy_item_text},
};
use tauri::{AppHandle, Manager, PhysicalPosition};
use tauri_specta::Event;

use crate::{
    events::{ItemCopied, OverlayState, SettingsChanged},
    ipc::IpcError,
    logic,
    state::AppState,
};

pub const OVERLAY: &str = "overlay";
pub const SETTINGS: &str = "settings";

/// Parse text, remember it, emit `item-copied` and show the overlay.
pub fn handle_item_text(
    app: &AppHandle,
    state: &AppState,
    text: &str,
    pinned: bool,
) -> Result<(ParsedItem, FilterSet), IpcError> {
    let settings = state.settings();
    let opts = logic::query_options(&settings, &state.league());
    let data = state.wait_data();
    let catalog = data.as_deref().map(|d| d as &dyn Catalog);
    let (item, filters) = logic::process_item_text(text, catalog, &opts).inspect_err(|e| state.record_error(e))?;
    *state.last_item.lock().expect("last item lock") = Some((item.clone(), filters.clone()));
    set_pinned(app, state, pinned);
    show_overlay(app, state);
    if let Err(e) = (ItemCopied { item: item.clone(), filters: filters.clone(), pinned }).emit(app) {
        state.record_error(format!("emit item-copied: {e}"));
    }
    Ok((item, filters))
}

/// Full hotkey flow: copy from the game, then [`handle_item_text`].
pub async fn run_price_check(
    app: AppHandle,
    state: Arc<AppState>,
    pinned: bool,
) -> Result<(ParsedItem, FilterSet), IpcError> {
    // One copy at a time: an overlapping run would capture the other's item text as the
    // "previous" clipboard and restore it. The permit is held until the copy has finished.
    let Some(_permit) = state.flow_gate.try_begin(std::time::Instant::now()) else {
        return Err(IpcError::new(BUSY, "a price check is already running"));
    };
    let platform = state.platform.clone().ok_or_else(|| IpcError::new("platform", "no platform backend available"))?;
    if !logic::may_copy(platform.capabilities.window_tracking, || platform.window_tracker.is_game_focused()) {
        return Err(IpcError::new("no_item", NOT_FOCUSED));
    }
    let advanced = state.settings().hotkeys.advanced_copy;
    let text = tauri::async_runtime::spawn_blocking(move || {
        let opts = CopyOptions { advanced, ..CopyOptions::default() };
        copy_item_text(platform.input_simulator.as_ref(), platform.clipboard.as_ref(), opts, &std::thread::sleep)
    })
    .await
    .map_err(|e| IpcError::new("internal", e.to_string()))?
    .map_err(|e| {
        state.record_error(&e);
        IpcError::from(e)
    })?;
    handle_item_text(&app, &state, &text, pinned)
}

/// Error kind for a press dropped because another flow is running (or debounced).
pub const BUSY: &str = "busy";
/// Message of the `no_item` error when the hotkey fired outside the game.
pub const NOT_FOCUSED: &str = "game window is not focused";

/// Failures the overlay should not pop up for: dropped presses and presses outside the game.
pub fn is_silent_failure(e: &IpcError) -> bool {
    e.kind == BUSY || (e.kind == "no_item" && e.message == NOT_FOCUSED)
}

/// Spawn [`run_price_check`] without awaiting (hotkey, signal, tray, test hook).
pub fn trigger(app: &AppHandle, pinned: bool) {
    let app = app.clone();
    let state = Arc::clone(&app.state::<Arc<AppState>>());
    tauri::async_runtime::spawn(async move {
        if let Err(e) = run_price_check(app.clone(), state.clone(), pinned).await {
            if is_silent_failure(&e) {
                tracing::debug!("price check skipped: {e}");
                return;
            }
            // Surface copy/parse failures in the overlay instead of failing silently.
            set_pinned(&app, &state, pinned);
            show_overlay(&app, &state);
            let _ = (crate::events::PriceFailed { error: e }).emit(&app);
        }
    });
}

/// Record the pinned flag. On Windows the overlay is non-focusable (tao rewrites the ex-style on
/// `show()`, wiping `WS_EX_NOACTIVATE`) except while pinned, so the filters can be typed into.
pub fn set_pinned(app: &AppHandle, state: &AppState, pinned: bool) {
    // The panel takes focus in both modes now (Esc, scrolling, typing); pinning only disables
    // auto-hide and Esc-to-close.
    let _ = app;
    state.set_pinned(pinned);
}

pub fn emit_overlay_state(app: &AppHandle, state: &AppState) {
    let _ =
        (OverlayState { visible: state.overlay_visible.load(Ordering::SeqCst), pinned: state.is_pinned() }).emit(app);
}

/// Position the overlay beside the cursor and show it without stealing focus.
pub fn show_overlay(app: &AppHandle, state: &AppState) {
    let Some(win) = app.get_webview_window(OVERLAY) else { return };
    if let Ok(cursor) = app.cursor_position() {
        let size = win.outer_size().map(|s| (s.width as i32, s.height as i32)).unwrap_or((420, 560));
        let monitor =
            app.monitor_from_point(cursor.x, cursor.y).ok().flatten().or_else(|| app.primary_monitor().ok().flatten());
        if let Some(m) = monitor {
            let area = logic::Area {
                x: m.position().x,
                y: m.position().y,
                width: m.size().width as i32,
                height: m.size().height as i32,
            };
            let (x, y) = logic::overlay_position((cursor.x as i32, cursor.y as i32), size, area);
            // Layer-shell surfaces ignore set_position; anchor them to the output edge instead.
            // gtk-layer-shell calls GTK directly, which panics off the main thread (and
            // `show_overlay` runs on tokio workers from the hotkey/test-hook flows), so hop to
            // the main thread; the queued `show()` below is processed after it, in order.
            #[cfg(target_os = "linux")]
            if state.backend == kalandra_platform::BackendKind::Wayland {
                let right = x > area.x + area.width / 2;
                let side = if right { area.x + area.width - (x + size.0) } else { x - area.x };
                let (w, handle) = (win.clone(), app.clone());
                let anchored = app.run_on_main_thread(move || {
                    if let Err(e) = crate::overlay::linux::anchor_layer(&w, right, y - area.y, side.max(0)) {
                        handle.state::<Arc<AppState>>().record_error(format!("layer-shell anchor: {e}"));
                    }
                });
                if let Err(e) = anchored {
                    state.record_error(format!("layer-shell anchor: {e}"));
                }
            }
            let _ = win.set_position(PhysicalPosition::new(x, y));
        }
    }
    let _ = win.set_ignore_cursor_events(false);
    let _ = win.show();
    // A focused fullscreen game can sit above "always on top" windows (openbox and other X11
    // WMs), so explicitly stack the panel above the game and give it keyboard focus (Esc,
    // scrolling, filter editing). `hide_overlay` hands focus back to the game.
    if let Err(e) = crate::overlay::bring_to_front(&win, game_window(state).map(|g| g.native_id), true) {
        state.record_error(format!("overlay raise: {e}"));
    }
    state.overlay_visible.store(true, Ordering::SeqCst);
    emit_overlay_state(app, state);
}

pub fn hide_overlay(app: &AppHandle, state: &AppState) -> Result<(), IpcError> {
    if let Some(win) = app.get_webview_window(OVERLAY) {
        let had_focus = win.is_focused().unwrap_or(false);
        win.hide().map_err(|e| IpcError::new("internal", e.to_string()))?;
        if had_focus {
            crate::overlay::refocus_game(game_window(state).map(|g| g.native_id));
        }
    }
    state.overlay_visible.store(false, Ordering::SeqCst);
    set_pinned(app, state, false);
    emit_overlay_state(app, state);
    Ok(())
}

/// The tracked game window, when the backend can track it (not on Wayland-native).
pub fn game_window(state: &AppState) -> Option<kalandra_platform::WindowInfo> {
    let platform = state.platform.as_ref()?;
    if !platform.capabilities.window_tracking || state.backend == kalandra_platform::BackendKind::Wayland {
        return None;
    }
    platform.window_tracker.game_window().filter(|g| g.rect.width > 0 && g.rect.height > 0)
}

/// Move/resize `win` to exactly cover the game window. Tracker rects are physical pixels
/// everywhere except macOS, where Core Graphics reports points.
pub fn cover_game(win: &tauri::WebviewWindow, rect: kalandra_platform::Rect) {
    #[cfg(target_os = "macos")]
    {
        let _ = win.set_position(tauri::LogicalPosition::new(f64::from(rect.x), f64::from(rect.y)));
        let _ = win.set_size(tauri::LogicalSize::new(f64::from(rect.width), f64::from(rect.height)));
    }
    #[cfg(not(target_os = "macos"))]
    {
        let _ = win.set_position(PhysicalPosition::new(rect.x, rect.y));
        let _ = win.set_size(tauri::PhysicalSize::new(rect.width, rect.height));
    }
}

/// Default size of the settings window when it is not bound to the game.
const SETTINGS_SIZE: (f64, f64) = (900.0, 680.0);

/// Show the settings window on `route` (`settings`, `diagnostics`, `onboarding`).
///
/// With a tracked game window the settings become an overlay that covers the game exactly
/// (undecorated, transparent backdrop, card centred, kept above the game); otherwise it is a
/// normal decorated window.
pub fn show_settings(app: &AppHandle, route: &str) -> Result<(), IpcError> {
    let win = app.get_webview_window(SETTINGS).ok_or_else(|| IpcError::new("internal", "settings window missing"))?;
    let state = app.state::<Arc<AppState>>();
    let game = game_window(&state);
    let overlay = game.is_some();
    let was_overlay = state.settings_overlay.swap(overlay, Ordering::SeqCst);
    if let Some(g) = &game {
        let _ = win.set_decorations(false);
        let _ = win.set_shadow(false);
        let _ = win.set_skip_taskbar(true);
        cover_game(&win, g.rect);
    } else if was_overlay {
        let _ = win.set_always_on_top(false);
        let _ = win.set_skip_taskbar(false);
        let _ = win.set_decorations(true);
        let _ = win.set_size(tauri::LogicalSize::new(SETTINGS_SIZE.0, SETTINGS_SIZE.1));
        let _ = win.center();
    }
    let _ = win.eval(format!(
        "document.documentElement.dataset.overlay = '{}'; window.location.hash = '#/{route}'",
        if overlay { "1" } else { "0" }
    ));
    win.show().map_err(|e| IpcError::new("internal", e.to_string()))?;
    if let Some(g) = game {
        if let Err(e) = crate::overlay::bring_to_front(&win, Some(g.native_id), true) {
            state.record_error(format!("settings raise: {e}"));
        }
    } else {
        let _ = win.set_focus();
    }
    Ok(())
}

/// Hide the settings window (Esc / backdrop click in overlay mode, close button).
pub fn hide_settings(app: &AppHandle) -> Result<(), IpcError> {
    let win = app.get_webview_window(SETTINGS).ok_or_else(|| IpcError::new("internal", "settings window missing"))?;
    win.hide().map_err(|e| IpcError::new("internal", e.to_string()))
}

/// Keep the settings overlay glued to the game window while it moves or resizes; hide it when
/// the game window goes away.
pub fn spawn_game_follower(app: &AppHandle, state: &AppState) {
    let Some(platform) = state.platform.clone() else { return };
    if !platform.capabilities.window_tracking || state.backend == kalandra_platform::BackendKind::Wayland {
        return;
    }
    let rx = match platform.window_tracker.subscribe() {
        Ok(rx) => rx,
        Err(e) => {
            state.record_error(format!("game window tracking unavailable: {e}"));
            return;
        }
    };
    let app = app.clone();
    let spawned = std::thread::Builder::new().name("kalandra-game-follow".into()).spawn(move || {
        use kalandra_platform::WindowEvent;
        for ev in rx {
            let state = app.state::<Arc<AppState>>();
            if !state.settings_overlay.load(Ordering::SeqCst) {
                continue;
            }
            let Some(win) = app.get_webview_window(SETTINGS) else { continue };
            if !win.is_visible().unwrap_or(false) {
                continue;
            }
            match ev {
                WindowEvent::Found(info) => cover_game(&win, info.rect),
                WindowEvent::Moved { rect } => cover_game(&win, rect),
                WindowEvent::FocusChanged { .. } => {}
                WindowEvent::Lost => {
                    let _ = win.hide();
                }
            }
        }
    });
    if let Err(e) = spawned {
        state.record_error(format!("spawn game follower: {e}"));
    }
}

pub fn open_trade(app: &AppHandle, url: &str) -> Result<(), IpcError> {
    use tauri_plugin_opener::OpenerExt;
    let override_base = crate::state::env_override("KALANDRA_TRADE_BASE_URL");
    if !logic::is_allowed_trade_url(url, override_base.as_ref()) {
        return Err(IpcError::new("internal", format!("refusing to open non-trade url {url}")));
    }
    app.opener().open_url(url, None::<&str>).map_err(|e| IpcError::new("internal", e.to_string()))
}

/// (Re)register every hotkey from settings.
pub fn register_hotkeys(state: &AppState, settings: &Settings) -> Result<(), IpcError> {
    let Some(platform) = state.platform.as_ref() else { return Ok(()) };
    if !platform.capabilities.global_hotkeys {
        return Ok(());
    }
    platform.hotkeys.unregister_all()?;
    // One call so the Wayland portal binds (and confirms) all shortcuts together.
    platform.hotkeys.register_all(&[
        (HotkeyAction::PriceCheck, settings.hotkeys.price_check.clone()),
        (HotkeyAction::PriceCheckPinned, settings.hotkeys.price_check_pinned.clone()),
        (HotkeyAction::OpenSettings, settings.hotkeys.open_settings.clone()),
    ])?;
    Ok(())
}

/// Validate, register hotkeys (rolling back to the previous chords on failure), persist, swap
/// sources/settings and broadcast. Blocking (portal registration can take seconds): call it
/// off the main thread. Serialised by `AppState::apply_lock`.
pub fn apply_settings(app: &AppHandle, state: &AppState, settings: Settings) -> Result<Settings, IpcError> {
    kalandra_config::validate(&settings)?;
    let _guard = state.apply_lock.lock().unwrap_or_else(std::sync::PoisonError::into_inner);
    let previous = state.settings();
    let hotkeys_changed = previous.hotkeys != settings.hotkeys;
    let rollback = |e: IpcError| {
        if hotkeys_changed && let Err(re) = register_hotkeys(state, &previous) {
            state.record_error(format!("hotkey rollback failed: {re}"));
        }
        state.record_error(&e);
        e
    };
    if hotkeys_changed {
        register_hotkeys(state, &settings).map_err(&rollback)?;
    }
    let sources = state.sources().updated(&settings).map_err(|e| rollback(IpcError::new("config", e)))?;
    if let Some(paths) = state.paths.as_ref() {
        kalandra_config::save(paths, &settings).map_err(|e| rollback(e.into()))?;
    }
    if let Some(sources) = sources {
        *state.sources.write().expect("sources lock") = Arc::new(sources);
    }
    *state.settings.write().expect("settings lock") = settings.clone();
    if previous.ui.language != settings.ui.language {
        crate::tray::relabel(app, &settings.ui.language);
    }
    let _ = (SettingsChanged { settings: settings.clone() }).emit(app);
    Ok(settings)
}

/// Forward platform hotkey events to the flows. Runs on its own thread.
pub fn spawn_hotkey_loop(app: &AppHandle, state: &AppState) {
    let Some(platform) = state.platform.clone() else { return };
    let rx = match platform.hotkeys.events() {
        Ok(rx) => rx,
        Err(e) => {
            state.record_error(format!("hotkey events unavailable: {e}"));
            return;
        }
    };
    let app = app.clone();
    let spawned = std::thread::Builder::new().name("kalandra-hotkeys".into()).spawn(move || {
        for ev in rx {
            match ev.action {
                HotkeyAction::PriceCheck => trigger(&app, false),
                HotkeyAction::PriceCheckPinned => trigger(&app, true),
                HotkeyAction::OpenSettings => {
                    let _ = show_settings(&app, "settings");
                }
            }
        }
    });
    if let Err(e) = spawned {
        state.record_error(format!("spawn hotkey thread: {e}"));
    }
}

/// Key names that close an unpinned overlay when seen by the passive input hook.
pub fn is_close_key(key: &str) -> bool {
    matches!(key, "Escape" | "Esc")
}

/// Use the passive hook (where supported) so Esc closes the overlay even though it never
/// takes keyboard focus from the game.
pub fn spawn_input_loop(app: &AppHandle, state: &AppState) {
    let Some(platform) = state.platform.clone() else { return };
    if !platform.capabilities.passive_hook {
        return;
    }
    let rx = match platform.input_listener.start() {
        Ok(rx) => rx,
        Err(e) => {
            state.record_error(format!("passive input hook unavailable: {e}"));
            return;
        }
    };
    let app = app.clone();
    let spawned = std::thread::Builder::new().name("kalandra-input".into()).spawn(move || {
        for ev in rx {
            if let kalandra_platform::InputEvent::KeyDown { key } = ev
                && is_close_key(&key)
            {
                let state = app.state::<Arc<AppState>>();
                if state.overlay_visible.load(Ordering::SeqCst) && !state.is_pinned() {
                    let _ = hide_overlay(&app, &state);
                }
            }
        }
    });
    if let Err(e) = spawned {
        state.record_error(format!("spawn input thread: {e}"));
    }
}

/// Copy chord for the current settings (`Ctrl+C` / `Ctrl+Alt+C`).
pub fn copy_chord(advanced: bool) -> Chord {
    if advanced {
        Chord::new(&[KeyModifier::Ctrl, KeyModifier::Alt], "KeyC")
    } else {
        Chord::new(&[KeyModifier::Ctrl], "KeyC")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn close_keys() {
        assert!(is_close_key("Escape"));
        assert!(!is_close_key("KeyD"));
    }

    #[test]
    fn copy_chords() {
        assert_eq!(copy_chord(false).to_string(), "Ctrl+C");
        assert_eq!(copy_chord(true).to_string(), "Ctrl+Alt+C");
    }
}
