use std::sync::Mutex;

use kalandra_config::Settings;
use kalandra_core::{ParsedItem, query::FilterSet};

/// Shared app state managed by Tauri.
#[derive(Debug)]
pub struct AppState {
    pub settings: Mutex<Settings>,
    pub last_item: Mutex<Option<(ParsedItem, FilterSet)>>,
    pub pinned: Mutex<bool>,
}

impl AppState {
    pub fn new() -> Self {
        Self { settings: Mutex::new(Settings::default()), last_item: Mutex::new(None), pinned: Mutex::new(false) }
    }
}

impl Default for AppState {
    fn default() -> Self {
        Self::new()
    }
}
