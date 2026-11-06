//! Orchestration glue between platform, core, trade and the Tauri windows.
use std::sync::{Arc, atomic::Ordering};

use rkl_config::Settings;
use rkl_core::{ParsedItem, catalog::Catalog, query::FilterSet};
use rkl_platform::{
    Chord, HotkeyAction, KeyModifier,
    copy::{CopyOptions, copy_item_text},
};
use tauri::{AppHandle, Manager, PhysicalPosition};
use tauri_specta::Event;

use crate::{
    events::{ItemCopied, OverlayLayout, OverlayState, SettingsChanged},
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
    let (item, mut filters) = logic::process_item_text(text, catalog, &opts).inspect_err(|e| state.record_error(e))?;
    if settings.trade.remember_filters
        && let Ok(memory) = state.filter_memory.lock()
    {
        memory.apply(&mut filters);
    }
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

/// Zoom the price panel's web view (the `overlay.scale` setting); window sizes follow it in
/// [`layout_overlay`] and `resize_overlay`.
pub fn apply_panel_scale(app: &AppHandle, scale: f64) {
    if let Some(win) = app.get_webview_window(OVERLAY)
        && let Err(e) = win.set_zoom(scale)
    {
        tracing::warn!("panel scale: {e}");
    }
}

/// Round a card-sized panel window of `width` x `height` logical px.
pub fn round_card(win: &tauri::WebviewWindow, width: f64, height: f64, zoom: f64) {
    let scale = win.scale_factor().unwrap_or(1.0);
    let px = |v: f64| (v * scale).round().max(0.0) as u32;
    crate::overlay::round_window(win, (px(width), px(height)), px(crate::overlay::CARD_RADIUS * zoom));
}

/// Lay the price panel out with its card at `card` (screen, physical px).
///
/// Unpinned over a tracked game window, the window covers the game (dimmed backdrop, the card
/// drawn inside), so the pointer never enters the game and focus-follows-mouse WMs (openbox)
/// cannot hand focus back to it; a backdrop click closes the panel. Pinned (or without a tracked
/// game), the window is just the card, so the game stays usable around it.
pub fn layout_overlay(app: &AppHandle, state: &AppState, win: &tauri::WebviewWindow, card: (i32, i32)) {
    let scale = win.scale_factor().unwrap_or(1.0);
    let zoom = state.settings().overlay.scale;
    let card_height = state.overlay_card.lock().map_or(crate::state::CARD_HEIGHT, |c| c.1);
    if let Ok(mut c) = state.overlay_card.lock() {
        c.0 = card;
    }
    let game = if state.is_pinned() { None } else { game_window(state) };
    let layout = if let Some(g) = game {
        cover_game(win, g.rect);
        let (x, y) = logic::card_in_window(card, g.rect, scale, cfg!(target_os = "macos"));
        crate::overlay::round_window(win, (0, 0), 0);
        // The web view is zoomed by the panel scale: its CSS pixels are `zoom` logical pixels.
        OverlayLayout { cover: true, x: x / zoom, y: y / zoom }
    } else {
        let _ = win.set_size(tauri::LogicalSize::new(crate::state::CARD_WIDTH * zoom, card_height));
        round_card(win, crate::state::CARD_WIDTH * zoom, card_height, zoom);
        let _ = win.set_position(PhysicalPosition::new(card.0, card.1));
        OverlayLayout { cover: false, x: 0.0, y: 0.0 }
    };
    if let Ok(mut l) = state.overlay_layout.lock() {
        *l = layout;
    }
    let _ = layout.emit(app);
}

/// Position the overlay beside the cursor and show it without stealing focus.
pub fn show_overlay(app: &AppHandle, state: &AppState) {
    let Some(win) = app.get_webview_window(OVERLAY) else { return };
    let scale = win.scale_factor().unwrap_or(1.0);
    let card_height = state.overlay_card.lock().map_or(crate::state::CARD_HEIGHT, |c| c.1);
    let mut card = state.overlay_card.lock().map_or((0, 0), |c| c.0);
    if let Ok(cursor) = app.cursor_position() {
        let zoom = state.settings().overlay.scale;
        let size = ((crate::state::CARD_WIDTH * zoom * scale).round() as i32, (card_height * scale).round() as i32);
        let monitor =
            app.monitor_from_point(cursor.x, cursor.y).ok().flatten().or_else(|| app.primary_monitor().ok().flatten());
        if let Some(m) = monitor {
            let screen = logic::Area {
                x: m.position().x,
                y: m.position().y,
                width: m.size().width as i32,
                height: m.size().height as i32,
            };
            // Edges are the game window's when it is tracked (physical px, not macOS points).
            let area = game_window(state).filter(|_| !cfg!(target_os = "macos")).map_or(screen, |g| logic::Area {
                x: g.rect.x,
                y: g.rect.y,
                width: g.rect.width as i32,
                height: g.rect.height as i32,
            });
            let side = state.settings().overlay.side;
            let (x, y) = logic::panel_position(side, (cursor.x as i32, cursor.y as i32), size, area);
            // Layer-shell surfaces ignore set_position; anchor them to the output edge instead.
            // gtk-layer-shell calls GTK directly, which panics off the main thread (and
            // `show_overlay` runs on tokio workers from the hotkey/test-hook flows), so hop to
            // the main thread; the queued `show()` below is processed after it, in order.
            #[cfg(target_os = "linux")]
            if state.backend == rkl_platform::BackendKind::Wayland {
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
            card = (x, y);
        }
    }
    layout_overlay(app, state, &win, card);
    let _ = win.set_ignore_cursor_events(false);
    let _ = win.show();
    // A focused fullscreen game can sit above "always on top" windows (openbox and other X11
    // WMs), so explicitly stack the panel above the game and give it keyboard focus (Esc,
    // scrolling, filter editing). `hide_overlay` hands focus back to the game.
    let game_id = game_window(state).map(|g| g.native_id);
    if let Err(e) = crate::overlay::bring_to_front(&win, game_id, true) {
        state.record_error(format!("overlay raise: {e}"));
    }
    state.overlay_visible.store(true, Ordering::SeqCst);
    spawn_focus_guard(app, state, game_id);
    spawn_pinned_hover(app, state, game_id);
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

/// How long after showing the panel the focus guard watches for the game taking focus back.
const FOCUS_GUARD: std::time::Duration = std::time::Duration::from_millis(1500);
/// Re-focus attempts per show (avoid a focus tug-of-war with the game).
const MAX_REFOCUS: u32 = 3;

/// Linux/X11: Wine re-activates a fullscreen game shortly after it loses focus, and holds an X
/// pointer grab while it is active. Keep focus on the panel for a moment so Wine releases it.
#[cfg(all(target_os = "linux", feature = "native"))]
fn spawn_focus_guard(app: &AppHandle, state: &AppState, game_id: Option<u64>) {
    use rkl_platform::backend::x11;
    let Some(game_id) = game_id else { return };
    let seq = state.show_seq.fetch_add(1, Ordering::SeqCst) + 1;
    let app = app.clone();
    let spawned = std::thread::Builder::new().name("rkl-focus-guard".into()).spawn(move || {
        let start = std::time::Instant::now();
        let mut refocused = 0;
        while start.elapsed() < FOCUS_GUARD {
            std::thread::sleep(std::time::Duration::from_millis(100));
            let state = app.state::<Arc<AppState>>();
            if state.show_seq.load(Ordering::SeqCst) != seq || !state.overlay_visible.load(Ordering::SeqCst) {
                return;
            }
            let game_active = x11::active_window().ok().flatten() == Some(game_id);
            // A pinned panel lets the game have focus (hover it, focus-follows-mouse); only the
            // unpinned panel, which covers the game, is kept focused against Wine re-activating it.
            if game_active && !state.is_pinned() && refocused < MAX_REFOCUS {
                refocused += 1;
                tracing::debug!("game took focus back; re-focusing the panel ({refocused})");
                if let Some(win) = app.get_webview_window(OVERLAY) {
                    let _ = crate::overlay::bring_to_front(&win, Some(game_id), true);
                }
            }
        }
    });
    if let Err(e) = spawned {
        state.record_error(format!("spawn focus guard: {e}"));
    }
}

/// How long the cursor must rest on a pinned panel before it is focused.
const HOVER_FOCUS_DELAY: std::time::Duration = std::time::Duration::from_millis(150);
const HOVER_POLL: std::time::Duration = std::time::Duration::from_millis(50);

/// A pinned panel does not cover the game, and while a Proton/Wine game is focused it holds an
/// X pointer grab: the WM never sees the pointer enter the panel, so focus-follows-mouse
/// cannot focus it and clicks go to the game. Poll the cursor (the query works under a grab)
/// and activate the panel when the cursor rests on it; Wine then releases its grab.
fn spawn_pinned_hover(app: &AppHandle, state: &AppState, game_id: Option<u64>) {
    if game_id.is_none() || !state.is_pinned() {
        return;
    }
    let seq = state.show_seq.load(Ordering::SeqCst);
    let app = app.clone();
    let spawned = std::thread::Builder::new().name("rkl-pinned-hover".into()).spawn(move || {
        let mut inside_since: Option<std::time::Instant> = None;
        loop {
            std::thread::sleep(HOVER_POLL);
            let state = app.state::<Arc<AppState>>();
            if state.show_seq.load(Ordering::SeqCst) != seq
                || !state.overlay_visible.load(Ordering::SeqCst)
                || !state.is_pinned()
            {
                return;
            }
            let Some(win) = app.get_webview_window(OVERLAY) else { return };
            let (Ok(cursor), Ok(pos), Ok(size)) = (app.cursor_position(), win.outer_position(), win.outer_size())
            else {
                continue;
            };
            let inside = logic::point_in_rect(
                (cursor.x, cursor.y),
                (f64::from(pos.x), f64::from(pos.y), f64::from(size.width), f64::from(size.height)),
            );
            if !inside {
                inside_since = None;
                continue;
            }
            let since = *inside_since.get_or_insert_with(std::time::Instant::now);
            if since.elapsed() >= HOVER_FOCUS_DELAY && !win.is_focused().unwrap_or(false) {
                if let Err(e) = crate::overlay::bring_to_front(&win, game_id, true) {
                    state.record_error(format!("focus pinned panel: {e}"));
                }
                // Once per hover: leave and come back to focus it again.
                inside_since = Some(std::time::Instant::now() + std::time::Duration::from_secs(3600));
            }
        }
    });
    if let Err(e) = spawned {
        state.record_error(format!("spawn pinned hover: {e}"));
    }
}

#[cfg(not(all(target_os = "linux", feature = "native")))]
fn spawn_focus_guard(app: &AppHandle, state: &AppState, game_id: Option<u64>) {
    let _ = (app, state, game_id);
}

/// The tracked game window, when the backend can track it (not on Wayland-native).
pub fn game_window(state: &AppState) -> Option<rkl_platform::WindowInfo> {
    let platform = state.platform.as_ref()?;
    if !platform.capabilities.window_tracking || state.backend == rkl_platform::BackendKind::Wayland {
        return None;
    }
    platform.window_tracker.game_window().filter(|g| g.rect.width > 0 && g.rect.height > 0)
}

/// Move/resize `win` to exactly cover the game window. Tracker rects are physical pixels
/// everywhere except macOS, where Core Graphics reports points.
pub fn cover_game(win: &tauri::WebviewWindow, rect: rkl_platform::Rect) {
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
    if !platform.capabilities.window_tracking || state.backend == rkl_platform::BackendKind::Wayland {
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
    let spawned = std::thread::Builder::new().name("rkl-game-follow".into()).spawn(move || {
        use rkl_platform::WindowEvent;
        for ev in rx {
            let state = app.state::<Arc<AppState>>();
            // Clicking back into a fullscreen game makes WMs such as openbox raise it above
            // "always on top" windows; keep a pinned panel stacked above it (without taking
            // keyboard focus back from the game).
            if matches!(ev, WindowEvent::FocusChanged { focused: true })
                && state.is_pinned()
                && state.overlay_visible.load(Ordering::SeqCst)
                && state.settings().overlay.close_pinned_on_game_focus
            {
                let _ = hide_overlay(&app, &state);
                continue;
            }
            if matches!(ev, WindowEvent::FocusChanged { focused: true })
                && state.is_pinned()
                && state.overlay_visible.load(Ordering::SeqCst)
                && let Some(panel) = app.get_webview_window(OVERLAY)
                && let Err(e) = crate::overlay::bring_to_front(&panel, game_window(&state).map(|g| g.native_id), false)
            {
                state.record_error(format!("keep pinned panel on top: {e}"));
            }
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
    let override_base = crate::state::env_override("RKL_TRADE_BASE_URL");
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
    // Cleared ("Not set") hotkeys are simply not registered.
    let bindings: Vec<_> = [
        (HotkeyAction::PriceCheck, settings.hotkeys.price_check.clone()),
        (HotkeyAction::PriceCheckPinned, settings.hotkeys.price_check_pinned.clone()),
        (HotkeyAction::OpenSettings, settings.hotkeys.open_settings.clone()),
        (HotkeyAction::OpenTrade, settings.hotkeys.open_trade.clone()),
    ]
    .into_iter()
    .filter(|(_, chord)| !chord.is_unbound())
    .collect();
    platform.hotkeys.register_all(&bindings)?;
    Ok(())
}

/// Validate, register hotkeys (rolling back to the previous chords on failure), persist, swap
/// sources/settings and broadcast. Blocking (portal registration can take seconds): call it
/// off the main thread. Serialised by `AppState::apply_lock`.
pub fn apply_settings(app: &AppHandle, state: &AppState, settings: Settings) -> Result<Settings, IpcError> {
    rkl_config::validate(&settings)?;
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
    if previous.ui.autostart != settings.ui.autostart {
        crate::autostart::set_enabled(settings.ui.autostart)
            .map_err(|e| rollback(IpcError::new("platform", format!("start at login: {e}"))))?;
    }
    if let Some(paths) = state.paths.as_ref() {
        rkl_config::save(paths, &settings).map_err(|e| rollback(e.into()))?;
    }
    if let Some(sources) = sources {
        *state.sources.write().expect("sources lock") = Arc::new(sources);
    }
    *state.settings.write().expect("settings lock") = settings.clone();
    if previous.ui.language != settings.ui.language {
        crate::tray::relabel(app, &settings.ui.language);
    }
    if (previous.overlay.scale - settings.overlay.scale).abs() > f64::EPSILON {
        apply_panel_scale(app, settings.overlay.scale);
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
    let spawned = std::thread::Builder::new().name("rkl-hotkeys".into()).spawn(move || {
        for ev in rx {
            let state = app.state::<Arc<AppState>>();
            let overlay_open = state.overlay_visible.load(Ordering::SeqCst);
            let settings_open = app.get_webview_window(SETTINGS).and_then(|w| w.is_visible().ok()).unwrap_or(false);
            match logic::hotkey_effect(ev.action, overlay_open, settings_open) {
                logic::HotkeyEffect::PriceCheck { pinned } => trigger(&app, pinned),
                logic::HotkeyEffect::HideOverlay => {
                    let _ = hide_overlay(&app, &state);
                }
                logic::HotkeyEffect::ShowSettings => {
                    let _ = show_settings(&app, "settings");
                }
                logic::HotkeyEffect::HideSettings => {
                    let _ = hide_settings(&app);
                }
                logic::HotkeyEffect::OpenTrade => {
                    let url = state.last_price.lock().ok().and_then(|p| p.as_ref().and_then(|s| s.trade_url.clone()));
                    match url {
                        Some(url) => {
                            if let Err(e) = open_trade(&app, &url) {
                                state.record_error(&e);
                            }
                        }
                        None => state.record_error("open on trade site: no price check with a trade search yet"),
                    }
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
    let spawned = std::thread::Builder::new().name("rkl-input".into()).spawn(move || {
        for ev in rx {
            if let rkl_platform::InputEvent::KeyDown { key } = ev
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
