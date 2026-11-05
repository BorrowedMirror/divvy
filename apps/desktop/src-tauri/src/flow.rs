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
    state::{AppState, Sources},
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
    let catalog = state.data.as_deref().map(|d| d as &dyn Catalog);
    let (item, filters) = logic::process_item_text(text, catalog, &opts).inspect_err(|e| state.record_error(e))?;
    *state.last_item.lock().expect("last item lock") = Some((item.clone(), filters.clone()));
    state.set_pinned(pinned);
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
    let platform = state.platform.clone().ok_or_else(|| IpcError::new("platform", "no platform backend available"))?;
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

/// Spawn [`run_price_check`] without awaiting (hotkey, tray, test hook).
pub fn trigger(app: &AppHandle, pinned: bool) {
    let app = app.clone();
    let state = Arc::clone(&app.state::<Arc<AppState>>());
    tauri::async_runtime::spawn(async move {
        if let Err(e) = run_price_check(app.clone(), state.clone(), pinned).await {
            let _ = (crate::events::PriceFailed { error: e }).emit(&app);
        }
    });
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
            let _ = win.set_position(PhysicalPosition::new(x, y));
        }
    }
    let _ = win.set_ignore_cursor_events(false);
    let _ = win.show();
    let _ = win.set_always_on_top(true);
    state.overlay_visible.store(true, Ordering::SeqCst);
    emit_overlay_state(app, state);
}

pub fn hide_overlay(app: &AppHandle, state: &AppState) -> Result<(), IpcError> {
    if let Some(win) = app.get_webview_window(OVERLAY) {
        win.hide().map_err(|e| IpcError::new("internal", e.to_string()))?;
    }
    state.overlay_visible.store(false, Ordering::SeqCst);
    state.set_pinned(false);
    emit_overlay_state(app, state);
    Ok(())
}

/// Show the settings window on `route` (`settings` or `diagnostics`).
pub fn show_settings(app: &AppHandle, route: &str) -> Result<(), IpcError> {
    let win = app.get_webview_window(SETTINGS).ok_or_else(|| IpcError::new("internal", "settings window missing"))?;
    let _ = win.eval(format!("window.location.hash = '#/{route}'"));
    win.show().map_err(|e| IpcError::new("internal", e.to_string()))?;
    let _ = win.set_focus();
    Ok(())
}

/// Only allow opening the official trade site (or the configured override in tests).
pub fn is_allowed_trade_url(url: &str) -> bool {
    let Ok(parsed) = url::Url::parse(url) else { return false };
    let override_host = std::env::var("KALANDRA_TRADE_BASE_URL")
        .ok()
        .and_then(|u| url::Url::parse(&u).ok())
        .and_then(|u| u.host_str().map(str::to_owned));
    match parsed.host_str() {
        Some("www.pathofexile.com" | "pathofexile.com") => parsed.scheme() == "https",
        Some(h) => override_host.as_deref() == Some(h),
        None => false,
    }
}

pub fn open_trade(app: &AppHandle, url: &str) -> Result<(), IpcError> {
    use tauri_plugin_opener::OpenerExt;
    if !is_allowed_trade_url(url) {
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
    for (action, chord) in [
        (HotkeyAction::PriceCheck, &settings.hotkeys.price_check),
        (HotkeyAction::PriceCheckPinned, &settings.hotkeys.price_check_pinned),
        (HotkeyAction::OpenSettings, &settings.hotkeys.open_settings),
    ] {
        platform.hotkeys.register(action, chord)?;
    }
    Ok(())
}

/// Validate + persist settings, rebuild sources, re-register hotkeys, broadcast.
pub fn apply_settings(app: &AppHandle, state: &AppState, settings: Settings) -> Result<Settings, IpcError> {
    kalandra_config::validate(&settings)?;
    if let Some(paths) = state.paths.as_ref() {
        kalandra_config::save(paths, &settings)?;
    }
    let sources = Sources::from_settings(&settings).map_err(|e| IpcError::new("config", e))?;
    *state.sources.write().expect("sources lock") = Arc::new(sources);
    *state.settings.write().expect("settings lock") = settings.clone();
    if let Err(e) = register_hotkeys(state, &settings) {
        state.record_error(&e);
        return Err(e);
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
    std::thread::Builder::new()
        .name("kalandra-hotkeys".into())
        .spawn(move || {
            for ev in rx {
                match ev.action {
                    HotkeyAction::PriceCheck => trigger(&app, false),
                    HotkeyAction::PriceCheckPinned => trigger(&app, true),
                    HotkeyAction::OpenSettings => {
                        let _ = show_settings(&app, "settings");
                    }
                }
            }
        })
        .expect("spawn hotkey thread");
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
    fn trade_url_allow_list() {
        assert!(is_allowed_trade_url("https://www.pathofexile.com/trade2/search/poe2/Standard/abc"));
        assert!(!is_allowed_trade_url("http://www.pathofexile.com/trade2"));
        assert!(!is_allowed_trade_url("https://evil.example.com/"));
        assert!(!is_allowed_trade_url("not a url"));
    }

    #[test]
    fn copy_chords() {
        assert_eq!(copy_chord(false).to_string(), "Ctrl+C");
        assert_eq!(copy_chord(true).to_string(), "Ctrl+Alt+C");
    }
}
