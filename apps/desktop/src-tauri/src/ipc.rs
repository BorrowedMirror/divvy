//! IPC commands. The web UI calls these only through generated `bindings.ts`.
use std::sync::Arc;

use kalandra_config::Settings;
use kalandra_core::{ParsedItem, pricing::PriceSummary, query::FilterSet};
use kalandra_platform::{BackendKind, Capabilities, Chord, HotkeyAction};
use kalandra_trade::{PriceRequest, types::League};
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, State};

use crate::{flow, state::AppState};

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

impl From<kalandra_trade::TradeError> for IpcError {
    fn from(e: kalandra_trade::TradeError) -> Self {
        let info = kalandra_trade::error::TradeErrorInfo::from(&e);
        let kind = if info.kind == "rate_limited" { "rate_limited" } else { "trade" };
        Self { kind: kind.into(), message: info.message, retry_after_ms: info.retry_after_ms }
    }
}

impl From<kalandra_config::ConfigError> for IpcError {
    fn from(e: kalandra_config::ConfigError) -> Self {
        Self::new("config", e.to_string())
    }
}

impl From<kalandra_platform::PlatformError> for IpcError {
    fn from(e: kalandra_platform::PlatformError) -> Self {
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
        capabilities: state.capabilities(),
        data_version: state
            .data()
            .map(|d| d.manifest.game_version.clone().unwrap_or_else(|| d.manifest.generated_at.clone())),
        league: Some(state.league()),
        settings_path: state.paths.as_ref().map(|p| p.settings_file.display().to_string()),
        log_path: state.log_path.get().map(|p| p.display().to_string()),
        recent_errors: state.errors.lock().expect("errors lock").iter().cloned().collect(),
    }
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

#[tauri::command]
#[specta::specta]
pub fn set_overlay_pinned(app: AppHandle, state: State<'_, Arc<AppState>>, pinned: bool) -> Result<(), IpcError> {
    flow::set_pinned(&app, &state, pinned);
    flow::emit_overlay_state(&app, &state);
    Ok(())
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
