//! rkl-config: settings schema (`settings.toml`), migrations, per-OS paths.
use std::path::{Path, PathBuf};

use rkl_core::pricing::PriceSourceKind;
use rkl_platform::{BackendKind, Chord};
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
    pub waystone: WaystoneSettings,
    pub chat: ChatSettings,
    pub stash: StashSettings,
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
    /// Open the last price check's search on the trade site (unset by default).
    pub open_trade: Chord,
    /// Use Ctrl+Alt+C (advanced item text with tiers) instead of Ctrl+C.
    pub advanced_copy: bool,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct TradeSettings {
    pub source: PriceSourceKind,
    /// Default seller filter for new searches: instant buyout, online, or any (default).
    pub sellers: rkl_core::query::OnlineStatus,
    pub widen_pct: f64,
    pub enable_all_stats: bool,
    /// Listings fetched per trade-site search (10 to 100, in steps of 10).
    pub listing_count: u32,
    /// Fetch currency rates (from poe2scout) to compare listings in other currencies and show
    /// each price's divine or exalted equivalent.
    pub convert_prices: bool,
    /// Start each item with the stats the user last had on or off for its category.
    pub remember_filters: bool,
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
    /// Show one row per seller (their cheapest listing, with a count).
    pub group_by_seller: bool,
    /// Listing table columns after the price, in order.
    pub listing_columns: Vec<ListingColumn>,
    /// Size of the price panel (0.75 to 1.5; 1 = normal).
    pub scale: f64,
    /// How dark the backdrop behind the unpinned panel is (0 = none, 0.9 = nearly black).
    pub backdrop_dim: f64,
    /// Close a pinned panel as soon as the game window gets focus again.
    pub close_pinned_on_game_focus: bool,
    /// Which lookup links the panel shows under the item name.
    pub links: LinkSettings,
    /// Where prices and listings go: below the filters, or in a column beside them.
    pub results: ResultsLayout,
}

/// Placement of the results (prices and listings) in the price panel.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum ResultsLayout {
    /// Under the Search button (one narrow column).
    Below,
    /// In a column to the left of the filters.
    Left,
    /// In a column to the right of the filters.
    Right,
    /// Beside the filters (right) for items with many stats, below otherwise.
    Auto,
}

/// Lookup links shown in the price panel.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct LinkSettings {
    pub poe2db: bool,
    pub wiki: bool,
    pub craft_of_exile: bool,
}

impl Default for LinkSettings {
    fn default() -> Self {
        Self { poe2db: true, wiki: true, craft_of_exile: true }
    }
}

/// Optional columns of the listing table (the price column is always shown).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum ListingColumn {
    Seller,
    ItemLevel,
    Stock,
    Listed,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum OverlaySide {
    /// Beside the cursor, flipping when near the screen edge.
    Cursor,
    Left,
    Right,
    /// Where the user last dragged the panel (beside the cursor until it has been moved).
    Last,
}

/// Most chat commands / stash searches that can be configured.
pub const MAX_MACROS: usize = 20;

/// A chat command sent with one hotkey: Enter, the text, and (with `send`) Enter again. `&&`
/// chains several lines in one key press; `@last` and `@area` are filled from the game log.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct ChatCommand {
    pub chord: Chord,
    pub text: String,
    /// Press Enter after the text (off: leave it in the chat box, e.g. `@last `).
    pub send: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct ChatSettings {
    pub commands: Vec<ChatCommand>,
    /// The game's `Client.txt`, read for `@last` and `@area`. Empty: found automatically.
    pub game_log: String,
}

impl Default for ChatSettings {
    fn default() -> Self {
        let cmd = |chord: Chord, text: &str| ChatCommand { chord, text: text.into(), send: true };
        Self {
            commands: vec![
                cmd(Chord::new(&[], "F5"), "/hideout"),
                cmd(Chord::unbound(), "/kingsmarch"),
                cmd(Chord::unbound(), "/exit"),
                cmd(Chord::unbound(), "@last ty&&/kick @last"),
                cmd(Chord::unbound(), "/invite @last"),
                cmd(Chord::unbound(), "/tradewith @last"),
                cmd(Chord::unbound(), "/hideout @last"),
            ],
            game_log: String::new(),
        }
    }
}

/// A saved stash search: the hotkey fills the stash (or vendor) search box with `text`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct StashSearch {
    pub chord: Chord,
    pub text: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct StashSettings {
    pub searches: Vec<StashSearch>,
    /// Ctrl + mouse wheel over the game switches stash tabs (one arrow key per wheel step).
    pub ctrl_scroll_tabs: bool,
}

/// How a waystone mod is flagged in the panel.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum ModFlag {
    Danger,
    Warn,
    Good,
}

/// Waystone mod check: flags per mod, keyed by the mod's template as the game copies it
/// (rolled numbers as `#`, e.g. `Monsters fire # additional Projectiles`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct WaystoneSettings {
    pub flags: std::collections::BTreeMap<String, ModFlag>,
}

/// Mods flagged dangerous out of the box (and by the "Common dangers" preset).
pub const DEFAULT_DANGEROUS_WAYSTONE_MODS: &[&str] = &[
    "Players have -#% to all maximum Resistances",
    "Players have #% less Recovery Rate of Life and Energy Shield",
    "Monsters fire # additional Projectiles",
];

impl Default for WaystoneSettings {
    fn default() -> Self {
        Self { flags: DEFAULT_DANGEROUS_WAYSTONE_MODS.iter().map(|m| ((*m).to_owned(), ModFlag::Danger)).collect() }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(default)]
pub struct UiSettings {
    /// BCP-47 tag, e.g. `en`.
    pub language: String,
    pub theme: Theme,
    pub start_minimised: bool,
    /// Check for and offer app updates in the background (release builds with an updater key only).
    pub check_updates: bool,
    /// First-run onboarding (permissions / platform notes) was completed or dismissed.
    pub onboarding_done: bool,
    /// Start RKL when the user logs in.
    pub autostart: bool,
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
            waystone: WaystoneSettings::default(),
            chat: ChatSettings::default(),
            stash: StashSettings::default(),
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
        use rkl_platform::KeyModifier::{Alt, Ctrl};
        Self {
            price_check: Chord::new(&[Ctrl], "KeyD"),
            price_check_pinned: Chord::new(&[Ctrl, Alt], "KeyD"),
            open_settings: Chord::new(&[Ctrl], "Space"),
            open_trade: Chord::unbound(),
            advanced_copy: true,
        }
    }
}

impl Default for TradeSettings {
    fn default() -> Self {
        Self {
            source: PriceSourceKind::Trade,
            sellers: rkl_core::query::OnlineStatus::Any,
            widen_pct: 10.0,
            enable_all_stats: false,
            listing_count: 20,
            convert_prices: true,
            remember_filters: true,
            contact: None,
            poesessid: None,
        }
    }
}

impl Default for OverlaySettings {
    fn default() -> Self {
        Self {
            auto_hide_ms: 400,
            opacity: 0.95,
            side: OverlaySide::Cursor,
            group_by_seller: true,
            listing_columns: vec![ListingColumn::Seller, ListingColumn::Listed],
            scale: 1.0,
            backdrop_dim: 0.6,
            close_pinned_on_game_focus: false,
            links: LinkSettings::default(),
            results: ResultsLayout::Below,
        }
    }
}

impl Default for UiSettings {
    fn default() -> Self {
        Self {
            language: "en".into(),
            theme: Theme::System,
            start_minimised: true,
            check_updates: true,
            onboarding_done: false,
            autostart: false,
        }
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

/// Environment variable that relocates every path under one root (portable mode, tests).
pub const CONFIG_DIR_ENV: &str = "RKL_CONFIG_DIR";

/// `directories::ProjectDirs` triple: `~/.config/rkl` (Linux), `%APPDATA%\\rkl` (Windows),
/// `~/Library/Application Support/app.rkl` (macOS). Matches the bundle identifier `app.rkl`.
pub const PROJECT_QUALIFIER: &str = "app";
pub const PROJECT_ORG: &str = "";
pub const PROJECT_APP: &str = "rkl";

/// Where settings lived before the project was renamed from kalandra (`au.nerv.kalandra`).
pub fn legacy_settings_file() -> Option<PathBuf> {
    directories::ProjectDirs::from("au", "nerv", "kalandra").map(|d| d.config_dir().join("settings.toml"))
}

/// One-time move to the new location: if `paths` has no settings file yet but `legacy` does,
/// copy it across (the old file is left in place). Returns whether a copy happened.
pub fn adopt_legacy_settings(paths: &Paths, legacy: Option<&Path>) -> Result<bool, ConfigError> {
    let Some(legacy) = legacy.filter(|l| l.is_file()) else { return Ok(false) };
    if paths.settings_file.exists() {
        return Ok(false);
    }
    std::fs::create_dir_all(&paths.config_dir).map_err(io_err(&paths.config_dir))?;
    std::fs::copy(legacy, &paths.settings_file).map_err(io_err(&paths.settings_file))?;
    Ok(true)
}

impl Paths {
    /// From `directories::ProjectDirs` (`app.rkl`), or `RKL_CONFIG_DIR` override.
    pub fn resolve() -> Result<Self, ConfigError> {
        Self::resolve_with(|key| std::env::var(key).ok())
    }

    /// Like [`Paths::resolve`], with an injectable environment lookup.
    pub fn resolve_with(env: impl Fn(&str) -> Option<String>) -> Result<Self, ConfigError> {
        if let Some(root) = env(CONFIG_DIR_ENV).filter(|v| !v.trim().is_empty()) {
            return Ok(Self::under(Path::new(&root)));
        }
        let dirs = directories::ProjectDirs::from(PROJECT_QUALIFIER, PROJECT_ORG, PROJECT_APP)
            .ok_or(ConfigError::NoConfigDir)?;
        let config_dir = dirs.config_dir().to_path_buf();
        Ok(Self {
            settings_file: config_dir.join("settings.toml"),
            config_dir,
            cache_dir: dirs.cache_dir().to_path_buf(),
            log_dir: dirs.state_dir().unwrap_or_else(|| dirs.data_local_dir()).join("logs"),
        })
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

fn invalid(field: impl Into<String>, reason: impl Into<String>) -> ConfigError {
    ConfigError::Invalid { field: field.into(), reason: reason.into() }
}

/// One migration step: upgrades a document from version `i` to `i + 1`, where `i` is the step's
/// index in [`MIGRATIONS`].
type MigrationStep = fn(toml::Table) -> Result<toml::Table, ConfigError>;

/// Ordered migration steps; `MIGRATIONS[i]` upgrades `vi -> v(i+1)`. To add v2, append a step
/// and bump [`SETTINGS_VERSION`].
const MIGRATIONS: &[MigrationStep] = &[migrate_v0_to_v1];

const _: () = assert!(MIGRATIONS.len() == SETTINGS_VERSION as usize, "one migration step per version");

fn document_version(doc: &toml::Table) -> Result<u32, ConfigError> {
    match doc.get("version") {
        None => Ok(0),
        Some(toml::Value::Integer(v)) => {
            u32::try_from(*v).map_err(|_| invalid("version", format!("{v} is not a valid version")))
        }
        Some(other) => Err(invalid("version", format!("expected an integer, found {}", other.type_str()))),
    }
}

/// Fetch (or create) the sub-table `name` of `doc`.
fn section<'a>(doc: &'a mut toml::Table, name: &str) -> Result<&'a mut toml::Table, ConfigError> {
    doc.entry(name)
        .or_insert_with(|| toml::Value::Table(toml::Table::new()))
        .as_table_mut()
        .ok_or_else(|| invalid(name, "expected a table"))
}

/// v0 (unversioned, pre-sections) stored `league`, `hotkey` and `online_only` at the top level:
///
/// ```toml
/// league = "Standard"
/// hotkey = "Ctrl+D"
/// online_only = false
/// ```
///
/// v1 moves them into `[league]`, `[hotkeys]` and `[trade]` (`online_only` becomes `sellers`). An explicit v0 league meant
/// "always use this league", i.e. auto-detect off; an empty one meant auto-detect.
fn migrate_v0_to_v1(mut doc: toml::Table) -> Result<toml::Table, ConfigError> {
    if let Some(value) = doc.remove("league") {
        match value {
            toml::Value::String(league) => {
                let sec = section(&mut doc, "league")?;
                if league.trim().is_empty() {
                    sec.insert("auto_detect".into(), true.into());
                } else {
                    sec.insert("auto_detect".into(), false.into());
                    sec.insert("league".into(), league.into());
                }
            }
            // Already sectioned, merely missing a version number.
            toml::Value::Table(t) => {
                doc.insert("league".into(), t.into());
            }
            other => return Err(invalid("league", format!("expected a string, found {}", other.type_str()))),
        }
    }
    if let Some(value) = doc.remove("hotkey") {
        let raw = value.as_str().ok_or_else(|| invalid("hotkey", "expected a string"))?;
        let chord = Chord::parse(raw).map_err(|e| invalid("hotkey", e.to_string()))?;
        let chord = toml::Table::try_from(chord)?;
        section(&mut doc, "hotkeys")?.insert("price_check".into(), chord.into());
    }
    if let Some(value) = doc.remove("online_only") {
        let online = value.as_bool().ok_or_else(|| invalid("online_only", "expected a boolean"))?;
        section(&mut doc, "trade")?.insert("sellers".into(), (if online { "online" } else { "any" }).into());
    }
    doc.insert("version".into(), 1.into());
    Ok(doc)
}

/// Upgrade a raw TOML document from any older version to [`SETTINGS_VERSION`].
pub fn migrate(mut doc: toml::Table) -> Result<toml::Table, ConfigError> {
    let found = document_version(&doc)?;
    if found > SETTINGS_VERSION {
        return Err(ConfigError::TooNew { found });
    }
    for step in &MIGRATIONS[found as usize..] {
        doc = step(doc)?;
    }
    Ok(doc)
}

/// `KeyboardEvent.code` names (and common aliases) that are modifiers, not chord keys.
const MODIFIER_KEY_CODES: &[&str] = &[
    "Control",
    "ControlLeft",
    "ControlRight",
    "Ctrl",
    "Alt",
    "AltLeft",
    "AltRight",
    "AltGraph",
    "Option",
    "Shift",
    "ShiftLeft",
    "ShiftRight",
    "Meta",
    "MetaLeft",
    "MetaRight",
    "OSLeft",
    "OSRight",
    "Cmd",
    "Super",
    "Win",
];

fn validate_chord(field: &str, chord: &Chord) -> Result<(), ConfigError> {
    let key = chord.key.trim();
    if key.is_empty() {
        // An explicitly cleared hotkey ("Not set"); modifiers alone are not a chord.
        return if chord.modifiers.is_empty() { Ok(()) } else { Err(invalid(field, "chord has no key")) };
    }
    if MODIFIER_KEY_CODES.iter().any(|m| m.eq_ignore_ascii_case(key)) {
        return Err(invalid(field, format!("`{key}` is a modifier; a chord needs a non-modifier key")));
    }
    Ok(())
}

/// Canonical form for comparing chords: modifier order and duplicates don't matter.
fn chord_key(chord: &Chord) -> (Vec<rkl_platform::KeyModifier>, String) {
    let mut mods = chord.modifiers.clone();
    mods.sort_by_key(|m| *m as u8);
    mods.dedup();
    (mods, chord.key.trim().to_owned())
}

fn looks_like_email(s: &str) -> bool {
    let Some((local, domain)) = s.split_once('@') else { return false };
    !local.is_empty()
        && !domain.contains('@')
        && s.chars().all(|c| c.is_ascii_graphic() && !matches!(c, ';' | '(' | ')' | ',' | '<' | '>'))
        && domain.split('.').count() >= 2
        && domain.split('.').all(|label| !label.is_empty())
}

/// Validate semantic constraints (ranges, chords distinct, ...).
pub fn validate(s: &Settings) -> Result<(), ConfigError> {
    if s.version > SETTINGS_VERSION {
        return Err(ConfigError::TooNew { found: s.version });
    }

    if !s.league.auto_detect && s.league.league.as_deref().is_none_or(|l| l.trim().is_empty()) {
        return Err(invalid("league.league", "must be set when league.auto_detect is false"));
    }

    if s.chat.commands.len() > MAX_MACROS || s.stash.searches.len() > MAX_MACROS {
        return Err(invalid("chat.commands", format!("at most {MAX_MACROS} chat commands and stash searches")));
    }
    for (field, text) in s
        .chat
        .commands
        .iter()
        .map(|c| ("chat.commands", &c.text))
        .chain(s.stash.searches.iter().map(|c| ("stash.searches", &c.text)))
    {
        if text.len() > 200 || text.contains('\n') {
            return Err(invalid(field, "text must be one line of at most 200 characters"));
        }
    }
    if s.chat.game_log.len() > 1024 || s.chat.game_log.contains('\n') {
        return Err(invalid("chat.game_log", "the game log path must be one line of at most 1024 characters"));
    }
    let mut hotkeys: Vec<(String, &Chord)> = vec![
        ("hotkeys.price_check".into(), &s.hotkeys.price_check),
        ("hotkeys.price_check_pinned".into(), &s.hotkeys.price_check_pinned),
        ("hotkeys.open_settings".into(), &s.hotkeys.open_settings),
        ("hotkeys.open_trade".into(), &s.hotkeys.open_trade),
    ];
    hotkeys.extend(s.chat.commands.iter().enumerate().map(|(i, c)| (format!("chat.commands[{i}]"), &c.chord)));
    hotkeys.extend(s.stash.searches.iter().enumerate().map(|(i, c)| (format!("stash.searches[{i}]"), &c.chord)));
    for (i, (field, chord)) in hotkeys.iter().enumerate() {
        validate_chord(field, chord)?;
        if let Some((other, _)) =
            hotkeys[..i].iter().filter(|_| !chord.is_unbound()).find(|(_, c)| chord_key(c) == chord_key(chord))
        {
            return Err(invalid(field, format!("`{chord}` is already bound to {other}")));
        }
    }

    if s.waystone.flags.len() > 500 || s.waystone.flags.keys().any(|k| k.trim().is_empty() || k.len() > 300) {
        return Err(invalid("waystone.flags", "at most 500 flagged mods of 1-300 characters"));
    }
    if !(0.75..=1.5).contains(&s.overlay.scale) {
        return Err(invalid("overlay.scale", format!("{} is not within 0.75..=1.5", s.overlay.scale)));
    }
    if !(0.0..=0.9).contains(&s.overlay.backdrop_dim) {
        return Err(invalid("overlay.backdrop_dim", format!("{} is not within 0..=0.9", s.overlay.backdrop_dim)));
    }
    if !(10..=100).contains(&s.trade.listing_count) || !s.trade.listing_count.is_multiple_of(10) {
        return Err(invalid("trade.listing_count", format!("{} is not 10, 20, ... 100", s.trade.listing_count)));
    }
    if (1..s.overlay.listing_columns.len())
        .any(|i| s.overlay.listing_columns[..i].contains(&s.overlay.listing_columns[i]))
    {
        return Err(invalid("overlay.listing_columns", "a column is listed twice"));
    }
    if !(0.0..=100.0).contains(&s.trade.widen_pct) {
        return Err(invalid("trade.widen_pct", format!("{} is not within 0..=100", s.trade.widen_pct)));
    }
    if let Some(contact) = s.trade.contact.as_deref().filter(|c| !looks_like_email(c)) {
        return Err(invalid("trade.contact", format!("`{contact}` does not look like an e-mail address")));
    }

    if let Some(sid) = s.trade.poesessid.as_deref().filter(|v| !v.is_empty())
        && !sid.chars().all(|c| c.is_ascii_alphanumeric())
    {
        return Err(invalid("trade.poesessid", "must contain only ASCII letters and digits"));
    }

    if !(0.1..=1.0).contains(&s.overlay.opacity) {
        return Err(invalid("overlay.opacity", format!("{} is not within 0.1..=1.0", s.overlay.opacity)));
    }
    if s.overlay.auto_hide_ms > 10_000 {
        return Err(invalid("overlay.auto_hide_ms", format!("{} exceeds 10000", s.overlay.auto_hide_ms)));
    }
    Ok(())
}

fn io_err(path: &Path) -> impl FnOnce(std::io::Error) -> ConfigError + '_ {
    move |source| ConfigError::Io { path: path.to_path_buf(), source }
}

/// Load settings; missing file -> defaults. Runs [`migrate`] then [`validate`].
pub fn load(paths: &Paths) -> Result<Settings, ConfigError> {
    let path = &paths.settings_file;
    let text = match std::fs::read_to_string(path) {
        Ok(text) => text,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Settings::default()),
        Err(e) => return Err(io_err(path)(e)),
    };
    let doc: toml::Table = toml::from_str(&text)?;
    let settings: Settings = migrate(doc)?.try_into()?;
    validate(&settings)?;
    Ok(settings)
}

/// Per-process counter making each save's temp file name unique.
static SAVE_SEQ: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);

/// Atomically write settings (temp file in the same directory, fsync, rename).
pub fn save(paths: &Paths, settings: &Settings) -> Result<(), ConfigError> {
    use std::io::Write as _;

    validate(settings)?;
    // Always persist the current schema version so a stale `version` sent over IPC does not
    // make every later `load` re-run old migrations.
    let mut current = settings.clone();
    current.version = SETTINGS_VERSION;
    let text = toml::to_string_pretty(&current)?;
    let target = &paths.settings_file;
    let dir = target.parent().filter(|p| !p.as_os_str().is_empty()).unwrap_or_else(|| Path::new("."));
    std::fs::create_dir_all(dir).map_err(io_err(dir))?;

    let file_name = target.file_name().map_or_else(|| "settings.toml".into(), |n| n.to_string_lossy().into_owned());
    // Unique per call so concurrent saves in one process never share a temp file.
    let seq = SAVE_SEQ.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    let tmp = dir.join(format!(".{file_name}.{}.{seq}.tmp", std::process::id()));
    let write = || -> std::io::Result<()> {
        let mut opts = std::fs::OpenOptions::new();
        opts.write(true).create_new(true);
        // The file may hold a POESESSID: keep it private to the user.
        #[cfg(unix)]
        std::os::unix::fs::OpenOptionsExt::mode(&mut opts, 0o600);
        let mut f = opts.open(&tmp)?;
        f.write_all(text.as_bytes())?;
        f.sync_all()?;
        std::fs::rename(&tmp, target)
    };
    if let Err(e) = write() {
        let _ = std::fs::remove_file(&tmp);
        return Err(io_err(target)(e));
    }
    // Persist the rename itself; best effort, not every platform/filesystem supports it.
    #[cfg(unix)]
    if let Ok(d) = std::fs::File::open(dir) {
        let _ = d.sync_all();
    }
    Ok(())
}
