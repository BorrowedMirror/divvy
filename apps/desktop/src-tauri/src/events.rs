//! Events pushed from Rust to the web UI. Exported to `web/src/bindings.ts`.
use rkl_config::Settings;
use rkl_core::{ParsedItem, pricing::PriceSummary, query::FilterSet};
use serde::{Deserialize, Serialize};

use crate::ipc::IpcError;

/// An item was copied from the game and parsed; overlay should show it and start querying.
#[derive(Debug, Clone, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct ItemCopied {
    pub item: ParsedItem,
    pub filters: FilterSet,
    pub pinned: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct PriceReady {
    pub summary: PriceSummary,
}

#[derive(Debug, Clone, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct PriceFailed {
    pub error: IpcError,
}

#[derive(Debug, Clone, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct SettingsChanged {
    pub settings: Settings,
}

#[derive(Debug, Clone, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct OverlayState {
    pub visible: bool,
    pub pinned: bool,
}

/// The game holds an X pointer grab (Wine fullscreen mouse capture) while the panel is open, so
/// clicks cannot reach it. The panel shows how to fix it.
#[derive(Debug, Clone, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct MouseCaptured {
    pub captured: bool,
}
