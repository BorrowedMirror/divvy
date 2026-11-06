//! IPC commands. The web UI calls these only through generated `bindings.ts`.
use std::sync::Arc;

use rkl_config::Settings;
use rkl_core::{ParsedItem, pricing::PriceSummary, query::FilterSet};
use rkl_platform::{BackendKind, Capabilities, Chord, HotkeyAction};
use rkl_trade::{PriceRequest, types::League};
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, State};

use crate::{events::ItemCopied, flow, state::AppState};

/// Error shape for every command.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type, thiserror::Error)]
#[error("{kind}: {message}")]
pub struct IpcError {
    /// `parse`, `trade`, `rate_limited`, `config`, `platform`, `no_item`, `busy`, `update`, `internal`.
    pub kind: String,
    pub message: String,
    pub retry_after_ms: Option<u64>,
}

impl IpcError {
    pub fn new(kind: &str, message: impl Into<String>) -> Self {
        Self { kind: kind.into(), message: message.into(), retry_after_ms: None }
    }
}

impl From<rkl_trade::TradeError> for IpcError {
    fn from(e: rkl_trade::TradeError) -> Self {
        let info = rkl_trade::error::TradeErrorInfo::from(&e);
        let kind = if info.kind == "rate_limited" { "rate_limited" } else { "trade" };
        Self { kind: kind.into(), message: info.message, retry_after_ms: info.retry_after_ms }
    }
}

impl From<rkl_config::ConfigError> for IpcError {
    fn from(e: rkl_config::ConfigError) -> Self {
        Self::new("config", e.to_string())
    }
}

impl From<rkl_platform::PlatformError> for IpcError {
    fn from(e: rkl_platform::PlatformError) -> Self {
        Self::new("platform", e.to_string())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, specta::Type)]
pub struct Diagnostics {
    pub version: String,
    pub os: String,
    pub backend: BackendKind,
    pub capabilities: Capabilities,
    pub data_version: Option<String>,
    pub league: Option<String>,
    pub settings_path: Option<String>,
    pub log_path: Option<String>,
    pub recent_errors: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, specta::Type)]
pub struct PriceCheckRequest {
    /// Item text to parse directly (debug / paste). `None` = run the copy flow against the game.
    pub text: Option<String>,
    pub pinned: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, specta::Type)]
pub struct PriceCheckResponse {
    pub item: ParsedItem,
    pub filters: FilterSet,
}

#[tauri::command]
#[specta::specta]
pub fn get_settings(state: State<'_, Arc<AppState>>) -> Settings {
    state.settings()
}

#[tauri::command]
#[specta::specta]
pub async fn save_settings(
    app: AppHandle,
    state: State<'_, Arc<AppState>>,
    settings: Settings,
) -> Result<Settings, IpcError> {
    apply_off_main(app, Arc::clone(&state), settings).await
}

/// Sync Tauri commands run on the main thread, and hotkey registration (XDG portal) can block
/// for seconds, so settings are applied on the blocking pool.
async fn apply_off_main(app: AppHandle, state: Arc<AppState>, settings: Settings) -> Result<Settings, IpcError> {
    tauri::async_runtime::spawn_blocking(move || flow::apply_settings(&app, &state, settings))
        .await
        .map_err(|e| IpcError::new("internal", e.to_string()))?
}

#[tauri::command]
#[specta::specta]
pub fn get_diagnostics(state: State<'_, Arc<AppState>>) -> Diagnostics {
    Diagnostics {
        version: env!("CARGO_PKG_VERSION").into(),
        os: std::env::consts::OS.into(),
        backend: state.backend,
        capabilities: state.capabilities_with_notes(),
        data_version: state
            .data()
            .map(|d| d.manifest.game_version.clone().unwrap_or_else(|| d.manifest.generated_at.clone())),
        league: Some(state.league()),
        settings_path: state.paths.as_ref().map(|p| p.settings_file.display().to_string()),
        log_path: state.log_path.get().map(|p| p.display().to_string()),
        recent_errors: state.errors.lock().expect("errors lock").iter().cloned().collect(),
    }
}

/// Currency icons (exchange tag -> name and GGG CDN image URL) for showing prices as icons.
/// Served from the trade site's `data/static`, cached by the trade client.
#[tauri::command]
#[specta::specta]
pub async fn currency_icons(state: State<'_, Arc<AppState>>) -> Result<Vec<rkl_trade::client::CurrencyIcon>, IpcError> {
    Ok(state.sources().trade.currency_icons().await?)
}

#[tauri::command]
#[specta::specta]
pub async fn list_leagues(state: State<'_, Arc<AppState>>) -> Result<Vec<League>, IpcError> {
    let leagues = state.sources().trade.leagues().await?;
    *state.leagues.lock().expect("leagues lock") = Some(leagues.clone());
    Ok(leagues)
}

#[tauri::command]
#[specta::specta]
pub async fn price_check(
    app: AppHandle,
    state: State<'_, Arc<AppState>>,
    request: PriceCheckRequest,
) -> Result<PriceCheckResponse, IpcError> {
    let state = Arc::clone(&state);
    let (item, filters) = match request.text {
        Some(text) => flow::handle_item_text(&app, &state, &text, request.pinned)?,
        None => flow::run_price_check(app.clone(), state, request.pinned).await?,
    };
    Ok(PriceCheckResponse { item, filters })
}

#[tauri::command]
#[specta::specta]
pub async fn query_price(state: State<'_, Arc<AppState>>, filters: FilterSet) -> Result<PriceSummary, IpcError> {
    let item = state
        .last_item
        .lock()
        .expect("last item lock")
        .as_ref()
        .map(|(i, _)| i.clone())
        .ok_or_else(|| IpcError::new("no_item", "no item has been copied yet"))?;
    let source = state.sources().pick(state.settings().trade.source, &item);
    let req = PriceRequest { item, filters };
    match source.price(&req).await {
        Ok(summary) => {
            *state.last_price.lock().expect("last price lock") = Some(summary.clone());
            Ok(summary)
        }
        Err(e) => {
            state.record_error(&e);
            Err(e.into())
        }
    }
}

#[tauri::command]
#[specta::specta]
pub fn open_trade(app: AppHandle, url: String) -> Result<(), IpcError> {
    flow::open_trade(&app, &url)
}

#[tauri::command]
#[specta::specta]
pub fn hide_overlay(app: AppHandle, state: State<'_, Arc<AppState>>) -> Result<(), IpcError> {
    flow::hide_overlay(&app, &state)
}

/// The last copied item as an `item-copied` payload. The overlay calls it once its listeners
/// are registered, so an item copied before the webview was listening is not lost.
#[tauri::command]
#[specta::specta]
pub fn get_overlay_snapshot(state: State<'_, Arc<AppState>>) -> Option<ItemCopied> {
    overlay_snapshot(&state)
}

pub fn overlay_snapshot(state: &AppState) -> Option<ItemCopied> {
    let last = state.last_item.lock().expect("last item lock").clone();
    last.map(|(item, filters)| ItemCopied { item, filters, pinned: state.is_pinned() })
}

#[tauri::command]
#[specta::specta]
pub fn set_overlay_pinned(app: AppHandle, state: State<'_, Arc<AppState>>, pinned: bool) -> Result<(), IpcError> {
    flow::set_pinned(&app, &state, pinned);
    // Switch between covering the game (unpinned) and a card-sized window (pinned) in place.
    if state.overlay_visible.load(std::sync::atomic::Ordering::SeqCst)
        && let Some(win) = tauri::Manager::get_webview_window(&app, flow::OVERLAY)
    {
        let card = state.overlay_card.lock().map_or((0, 0), |c| c.0);
        flow::layout_overlay(&app, &state, &win, card);
        if let Err(e) = crate::overlay::bring_to_front(&win, flow::game_window(&state).map(|g| g.native_id), true) {
            state.record_error(format!("overlay focus: {e}"));
        }
    }
    flow::emit_overlay_state(&app, &state);
    Ok(())
}

/// Resize the price panel window to fit its content (`height` in logical px, measured by the
/// web view). Width is unchanged; the height is clamped to the monitor and the window is kept
/// on screen.
#[tauri::command]
#[specta::specta]
pub fn resize_overlay(app: AppHandle, height: f64) -> Result<(), IpcError> {
    let win = tauri::Manager::get_webview_window(&app, flow::OVERLAY)
        .ok_or_else(|| IpcError::new("internal", "overlay window missing"))?;
    let internal = |e: tauri::Error| IpcError::new("internal", e.to_string());
    let scale = win.scale_factor().map_err(internal)?;
    let monitor = win.current_monitor().map_err(internal)?;
    let width = win.inner_size().map_err(internal)?.to_logical::<f64>(scale).width;
    let screen_h = monitor.as_ref().map_or(f64::INFINITY, |m| f64::from(m.size().height) / scale);
    let h = crate::logic::fit_overlay_height(height, screen_h);
    let state = tauri::Manager::state::<Arc<AppState>>(&app);
    if let Ok(mut c) = state.overlay_card.lock() {
        c.1 = h;
    }
    // Covering the game: the window stays game-sized; the web view places the card itself.
    if state.overlay_layout.lock().is_ok_and(|l| l.cover) {
        return Ok(());
    }
    win.set_size(tauri::LogicalSize::new(width, h)).map_err(internal)?;
    if let Some(m) = monitor
        && let Ok(pos) = win.outer_position()
    {
        let h_phys = (h * scale).round() as i32;
        let y = crate::logic::keep_on_screen_y(pos.y, h_phys, m.position().y, m.size().height as i32);
        if y != pos.y {
            win.set_position(tauri::PhysicalPosition::new(pos.x, y)).map_err(internal)?;
        }
    }
    Ok(())
}

/// Current price panel layout, for the web view to catch up on mount.
#[tauri::command]
#[specta::specta]
pub fn get_overlay_layout(state: State<'_, Arc<AppState>>) -> crate::events::OverlayLayout {
    state.overlay_layout.lock().map_or(crate::events::OverlayLayout { cover: false, x: 0.0, y: 0.0 }, |l| *l)
}

/// Hide the settings window (overlay mode: Esc or a click on the backdrop).
#[tauri::command]
#[specta::specta]
pub fn close_settings(app: AppHandle) -> Result<(), IpcError> {
    flow::hide_settings(&app)
}

#[tauri::command]
#[specta::specta]
pub fn open_settings(app: AppHandle) -> Result<(), IpcError> {
    flow::show_settings(&app, "settings")
}

#[tauri::command]
#[specta::specta]
pub async fn rebind_hotkey(
    app: AppHandle,
    state: State<'_, Arc<AppState>>,
    action: HotkeyAction,
    chord: Chord,
) -> Result<(), IpcError> {
    let mut settings = state.settings();
    match action {
        HotkeyAction::PriceCheck => settings.hotkeys.price_check = chord,
        HotkeyAction::PriceCheckPinned => settings.hotkeys.price_check_pinned = chord,
        HotkeyAction::OpenSettings => settings.hotkeys.open_settings = chord,
    }
    apply_off_main(app, Arc::clone(&state), settings).await.map(|_| ())
}
