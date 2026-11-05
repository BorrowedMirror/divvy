//! kalandra-config: settings schema (`settings.toml`), migrations, per-OS paths.
use std::path::{Path, PathBuf};

use kalandra_core::pricing::PriceSourceKind;
use kalandra_platform::{BackendKind, Chord};
use serde::{Deserialize, Serialize};

/// Current schema version. Increment and add a step to [`migrate`] on any breaking change.
pub const SETTINGS_VERSION: u32 = 1;

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct Settings {
    pub version: u32,
    pub league: LeagueSettings,
    pub hotkeys: HotkeySettings,
    pub trade: TradeSettings,
    pub overlay: OverlaySettings,
    pub ui: UiSettings,
    pub platform: PlatformSettings,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct LeagueSettings {
    /// Pick the current softcore trade league automatically.
    pub auto_detect: bool,
    /// League id used when `auto_detect` is false (or as last-known value).
    pub league: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct HotkeySettings {
    pub price_check: Chord,
    pub price_check_pinned: Chord,
    pub open_settings: Chord,
    /// Use Ctrl+Alt+C (advanced item text with tiers) instead of Ctrl+C.
    pub advanced_copy: bool,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct TradeSettings {
    pub source: PriceSourceKind,
    pub online_only: bool,
    pub widen_pct: f64,
    pub enable_all_stats: bool,
    /// Optional e-mail appended to the User-Agent, as GGG requests.
    pub contact: Option<String>,
    /// Optional POESESSID; stored in plain text, so off by default.
    pub poesessid: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct OverlaySettings {
    /// Auto-hide after mouse leaves (ms); pinned panels ignore this.
    pub auto_hide_ms: u32,
    pub opacity: f64,
    pub side: OverlaySide,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum OverlaySide {
    /// Beside the cursor, flipping when near the screen edge.
    Cursor,
    Left,
    Right,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct UiSettings {
    /// BCP-47 tag, e.g. `en`.
    pub language: String,
    pub theme: Theme,
    pub start_minimised: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum Theme {
    System,
    Dark,
    Light,
}

#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct PlatformSettings {
    pub backend_override: Option<BackendKind>,
}

impl Default for Settings {
    fn default() -> Self {
        Self {
            version: SETTINGS_VERSION,
            league: LeagueSettings::default(),
            hotkeys: HotkeySettings::default(),
            trade: TradeSettings::default(),
            overlay: OverlaySettings::default(),
            ui: UiSettings::default(),
            platform: PlatformSettings::default(),
        }
    }
}

impl Default for LeagueSettings {
    fn default() -> Self {
        Self { auto_detect: true, league: None }
    }
}

impl Default for HotkeySettings {
    fn default() -> Self {
        use kalandra_platform::KeyModifier::{Alt, Ctrl};
        Self {
            price_check: Chord::new(&[Ctrl], "KeyD"),
            price_check_pinned: Chord::new(&[Ctrl, Alt], "KeyD"),
            open_settings: Chord::new(&[Ctrl, Alt], "KeyS"),
            advanced_copy: true,
        }
    }
}

impl Default for TradeSettings {
    fn default() -> Self {
        Self {
            source: PriceSourceKind::Trade,
            online_only: true,
            widen_pct: 10.0,
            enable_all_stats: false,
            contact: None,
            poesessid: None,
        }
    }
}

impl Default for OverlaySettings {
    fn default() -> Self {
        Self { auto_hide_ms: 400, opacity: 0.95, side: OverlaySide::Cursor }
    }
}

impl Default for UiSettings {
    fn default() -> Self {
        Self { language: "en".into(), theme: Theme::System, start_minimised: true }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    #[error("io error at {path}: {source}")]
    Io { path: PathBuf, source: std::io::Error },
    #[error("invalid settings toml: {0}")]
    Parse(#[from] toml::de::Error),
    #[error("cannot serialise settings: {0}")]
    Serialise(#[from] toml::ser::Error),
    #[error("settings version {found} is newer than supported {SETTINGS_VERSION}")]
    TooNew { found: u32 },
    #[error("invalid setting `{field}`: {reason}")]
    Invalid { field: String, reason: String },
    #[error("no home/config directory could be determined")]
    NoConfigDir,
}

/// Resolved per-OS locations.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Paths {
    pub config_dir: PathBuf,
    pub cache_dir: PathBuf,
    pub log_dir: PathBuf,
    pub settings_file: PathBuf,
}

impl Paths {
    /// From `directories::ProjectDirs` (`au.nerv.kalandra`), or `KALANDRA_CONFIG_DIR` override.
    pub fn resolve() -> Result<Self, ConfigError> {
        todo!("workstream J (config)")
    }

    /// All paths rooted under `root` (tests, portable mode).
    pub fn under(root: &Path) -> Self {
        Self {
            config_dir: root.join("config"),
            cache_dir: root.join("cache"),
            log_dir: root.join("logs"),
            settings_file: root.join("config").join("settings.toml"),
        }
    }
}

/// Upgrade a raw TOML document from any older version to [`SETTINGS_VERSION`].
pub fn migrate(_doc: toml::Table) -> Result<toml::Table, ConfigError> {
    todo!("workstream J (config)")
}

/// Validate semantic constraints (ranges, chords distinct, ...).
pub fn validate(_s: &Settings) -> Result<(), ConfigError> {
    todo!("workstream J (config)")
}

/// Load settings; missing file -> defaults. Runs [`migrate`] then [`validate`].
pub fn load(_paths: &Paths) -> Result<Settings, ConfigError> {
    todo!("workstream J (config)")
}

/// Atomically write settings (temp file + rename).
pub fn save(_paths: &Paths, _settings: &Settings) -> Result<(), ConfigError> {
    todo!("workstream J (config)")
}
