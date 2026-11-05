//! IPC commands. The web UI calls these only through generated `bindings.ts`.
use kalandra_config::Settings;
use kalandra_core::{ParsedItem, pricing::PriceSummary, query::FilterSet};
use kalandra_platform::{BackendKind, Capabilities, Chord, HotkeyAction};
use kalandra_trade::types::League;
use serde::{Deserialize, Serialize};
use tauri::State;

use crate::state::AppState;

/// Error shape for every command.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type, thiserror::Error)]
#[error("{kind}: {message}")]
pub struct IpcError {
    /// `parse`, `trade`, `rate_limited`, `config`, `platform`, `no_item`, `internal`.
    pub kind: String,
    pub message: String,
    pub retry_after_ms: Option<u64>,
}

impl IpcError {
    pub fn new(kind: &str, message: impl Into<String>) -> Self {
        Self { kind: kind.into(), message: message.into(), retry_after_ms: None }
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
pub fn get_settings(state: State<'_, AppState>) -> Settings {
    state.settings.lock().expect("settings lock").clone()
}

#[tauri::command]
#[specta::specta]
pub fn save_settings(state: State<'_, AppState>, settings: Settings) -> Result<Settings, IpcError> {
    *state.settings.lock().expect("settings lock") = settings.clone();
    Ok(settings)
}

#[tauri::command]
#[specta::specta]
pub fn get_diagnostics() -> Diagnostics {
    Diagnostics {
        version: env!("CARGO_PKG_VERSION").into(),
        os: std::env::consts::OS.into(),
        backend: BackendKind::Fake,
        capabilities: Capabilities::default(),
        data_version: None,
        league: None,
        settings_path: None,
        log_path: None,
        recent_errors: vec![],
    }
}

#[tauri::command]
#[specta::specta]
pub async fn list_leagues() -> Result<Vec<League>, IpcError> {
    Err(IpcError::new("internal", "not wired yet (phase 3)"))
}

#[tauri::command]
#[specta::specta]
pub async fn price_check(request: PriceCheckRequest) -> Result<PriceCheckResponse, IpcError> {
    let _ = request;
    Err(IpcError::new("internal", "not wired yet (phase 3)"))
}

#[tauri::command]
#[specta::specta]
pub async fn query_price(filters: FilterSet) -> Result<PriceSummary, IpcError> {
    let _ = filters;
    Err(IpcError::new("internal", "not wired yet (phase 3)"))
}

#[tauri::command]
#[specta::specta]
pub fn open_trade(url: String) -> Result<(), IpcError> {
    let _ = url;
    Err(IpcError::new("internal", "not wired yet (phase 3)"))
}

#[tauri::command]
#[specta::specta]
pub fn hide_overlay() -> Result<(), IpcError> {
    Ok(())
}

#[tauri::command]
#[specta::specta]
pub fn set_overlay_pinned(state: State<'_, AppState>, pinned: bool) -> Result<(), IpcError> {
    *state.pinned.lock().expect("pinned lock") = pinned;
    Ok(())
}

#[tauri::command]
#[specta::specta]
pub fn open_settings() -> Result<(), IpcError> {
    Ok(())
}

#[tauri::command]
#[specta::specta]
pub fn rebind_hotkey(action: HotkeyAction, chord: Chord) -> Result<(), IpcError> {
    let _ = (action, chord);
    Err(IpcError::new("internal", "not wired yet (phase 3)"))
}
