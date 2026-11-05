//! Long-lived services shared by commands, the hotkey loop, the tray and the test hook.
use std::{
    collections::VecDeque,
    sync::{
        Arc, Mutex, OnceLock, RwLock,
        atomic::{AtomicBool, Ordering},
    },
};

use rkl_config::{Paths, Settings};
use rkl_core::{ParsedItem, pricing::PriceSummary, query::FilterSet};
use rkl_data::GameData;
use rkl_platform::{BackendKind, Capabilities, Platform};
use rkl_trade::{
    PriceSource, TradeClient, TradeConfig, ninja::PoeNinjaSource, ratelimit::SystemClock, scout::Poe2ScoutSource,
    types::League,
};
use url::Url;

use crate::logic;

const MAX_ERRORS: usize = 50;

/// Network price sources built from settings.
#[derive(Debug)]
pub struct Sources {
    pub trade: Arc<TradeClient>,
    pub ninja: Arc<PoeNinjaSource>,
    pub scout: Arc<Poe2ScoutSource>,
}

/// Endpoint override from `key` (e2e / wiremock). Only debug or `test-hook` builds honour it, and
/// only for https or loopback URLs, so a release build can't be pointed at another host.
pub fn env_override(key: &str) -> Option<Url> {
    #[cfg(any(debug_assertions, feature = "test-hook"))]
    {
        let url = std::env::var(key).ok().and_then(|v| Url::parse(&v).ok())?;
        if logic::is_safe_override(&url) {
            return Some(url);
        }
        tracing::warn!("ignoring {key}: only https or loopback http URLs are allowed");
        None
    }
    #[cfg(not(any(debug_assertions, feature = "test-hook")))]
    {
        let _ = key;
        None
    }
}

fn env_url(key: &str, default: &str) -> Url {
    env_override(key).unwrap_or_else(|| Url::parse(default).expect("static url"))
}

fn trade_config(settings: &Settings) -> TradeConfig {
    TradeConfig {
        base_url: env_url("RKL_TRADE_BASE_URL", "https://www.pathofexile.com"),
        user_agent: logic::user_agent(settings),
        poesessid: settings.trade.poesessid.clone().filter(|s| !s.is_empty()),
        ..TradeConfig::default()
    }
}

fn ninja(ua: String) -> Arc<PoeNinjaSource> {
    Arc::new(PoeNinjaSource::new(env_url("RKL_NINJA_BASE_URL", "https://poe.ninja"), ua))
}

fn scout(ua: String) -> Arc<Poe2ScoutSource> {
    Arc::new(Poe2ScoutSource::new(env_url("RKL_SCOUT_BASE_URL", "https://poe2scout.com"), ua))
}

impl Sources {
    pub fn from_settings(settings: &Settings) -> Result<Self, String> {
        let config = trade_config(settings);
        let ua = config.user_agent.clone();
        let trade = TradeClient::new(config, Box::new(SystemClock)).map_err(|e| e.to_string())?;
        Ok(Self { trade: Arc::new(trade), ninja: ninja(ua.clone()), scout: scout(ua) })
    }

    /// Sources for new `settings`, reusing every client whose inputs are unchanged so a save
    /// keeps the trade rate-limiter state and the price caches. `None` = nothing to rebuild.
    pub fn updated(&self, settings: &Settings) -> Result<Option<Self>, String> {
        let config = trade_config(settings);
        let old = self.trade.config();
        let ua_changed = old.user_agent != config.user_agent;
        let trade_changed = ua_changed || old.base_url != config.base_url || old.poesessid != config.poesessid;
        if !trade_changed {
            return Ok(None);
        }
        let ua = config.user_agent.clone();
        let trade = Arc::new(TradeClient::new(config, Box::new(SystemClock)).map_err(|e| e.to_string())?);
        let (ninja, scout) =
            if ua_changed { (ninja(ua.clone()), scout(ua)) } else { (self.ninja.clone(), self.scout.clone()) };
        Ok(Some(Self { trade, ninja, scout }))
    }

    /// The configured source if it supports the item, else the official trade API.
    pub fn pick(&self, kind: rkl_core::pricing::PriceSourceKind, item: &ParsedItem) -> Arc<dyn PriceSource> {
        use rkl_core::pricing::PriceSourceKind as K;
        let preferred: Arc<dyn PriceSource> = match kind {
            K::Trade => self.trade.clone(),
            K::PoeNinja => self.ninja.clone(),
            K::Poe2Scout => self.scout.clone(),
        };
        if preferred.supports(item) { preferred } else { self.trade.clone() }
    }
}

/// Shared app state managed by Tauri.
#[derive(Debug)]
pub struct AppState {
    pub paths: Option<Paths>,
    pub settings: RwLock<Settings>,
    /// Bundled game data, decoded on a background thread at startup (`None` inside = failed).
    pub data: OnceLock<Option<Arc<GameData>>>,
    pub sources: RwLock<Arc<Sources>>,
    pub platform: Option<Arc<Platform>>,
    pub backend: BackendKind,
    pub last_item: Mutex<Option<(ParsedItem, FilterSet)>>,
    pub last_price: Mutex<Option<PriceSummary>>,
    pub leagues: Mutex<Option<Vec<League>>>,
    pub errors: Mutex<VecDeque<String>>,
    pub pinned: AtomicBool,
    pub overlay_visible: AtomicBool,
    /// The settings window is currently shown as an overlay covering the game window.
    pub settings_overlay: AtomicBool,
    /// Last probe found the game holding the mouse while the panel was open (Wine capture).
    pub mouse_captured: AtomicBool,
    /// Incremented on every panel show, so a stale focus guard can tell it was superseded.
    pub show_seq: std::sync::atomic::AtomicU64,
    pub quitting: AtomicBool,
    /// Closing settings quits the app (set when there is no tray to quit from).
    pub close_quits: AtomicBool,
    /// Serialises copy flows (hotkey, signal, tray, test hook, IPC).
    pub flow_gate: Arc<logic::FlowGate>,
    /// Held for a whole settings apply (register hotkeys, persist, swap state).
    pub apply_lock: Mutex<()>,
    /// Set once the primary instance has opened its log file.
    pub log_path: OnceLock<std::path::PathBuf>,
}

impl AppState {
    pub fn new(
        paths: Option<Paths>,
        settings: Settings,
        platform: Option<Arc<Platform>>,
        backend: BackendKind,
    ) -> Result<Self, String> {
        let sources = Sources::from_settings(&settings)?;
        Ok(Self {
            paths,
            settings: RwLock::new(settings),
            data: OnceLock::new(),
            sources: RwLock::new(Arc::new(sources)),
            platform,
            backend,
            last_item: Mutex::new(None),
            last_price: Mutex::new(None),
            leagues: Mutex::new(None),
            errors: Mutex::new(VecDeque::new()),
            pinned: AtomicBool::new(false),
            overlay_visible: AtomicBool::new(false),
            settings_overlay: AtomicBool::new(false),
            mouse_captured: AtomicBool::new(false),
            show_seq: std::sync::atomic::AtomicU64::new(0),
            quitting: AtomicBool::new(false),
            close_quits: AtomicBool::new(false),
            flow_gate: Arc::default(),
            apply_lock: Mutex::new(()),
            log_path: OnceLock::new(),
        })
    }

    pub fn settings(&self) -> Settings {
        self.settings.read().expect("settings lock").clone()
    }

    pub fn sources(&self) -> Arc<Sources> {
        self.sources.read().expect("sources lock").clone()
    }

    pub fn capabilities(&self) -> Capabilities {
        self.platform.as_ref().map(|p| p.capabilities.clone()).unwrap_or_default()
    }

    /// Capabilities plus problems the hotkey host found after registration (diagnostics).
    pub fn capabilities_with_notes(&self) -> Capabilities {
        let mut caps = self.capabilities();
        if let Some(p) = self.platform.as_ref() {
            caps.notes.extend(p.hotkeys.notes());
        }
        caps
    }

    pub fn record_error(&self, e: impl std::fmt::Display) {
        tracing::warn!("{e}");
        let mut errs = self.errors.lock().expect("errors lock");
        if errs.len() == MAX_ERRORS {
            errs.pop_front();
        }
        errs.push_back(e.to_string());
    }

    pub fn league(&self) -> String {
        let settings = self.settings();
        let leagues = self.leagues.lock().expect("leagues lock").clone();
        logic::resolve_league(&settings, leagues.as_deref())
    }

    /// Game data if decoding has finished (diagnostics; never blocks).
    pub fn data(&self) -> Option<Arc<GameData>> {
        self.data.get().cloned().flatten()
    }

    /// Game data, waiting for the startup decode to finish (item parsing needs the catalog).
    pub fn wait_data(&self) -> Option<Arc<GameData>> {
        self.data.wait().clone()
    }

    /// Store the auto-detected league list unless a newer `list_leagues` result is already there.
    pub fn set_leagues_if_unset(&self, leagues: Vec<League>) {
        self.leagues.lock().expect("leagues lock").get_or_insert(leagues);
    }

    pub fn set_pinned(&self, pinned: bool) {
        self.pinned.store(pinned, Ordering::SeqCst);
    }

    pub fn is_pinned(&self) -> bool {
        self.pinned.load(Ordering::SeqCst)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn state() -> AppState {
        AppState::new(None, Settings::default(), None, BackendKind::X11).unwrap()
    }

    fn league(id: &str) -> League {
        League { id: id.into(), realm: None, text: None }
    }

    #[test]
    fn auto_detect_does_not_overwrite_newer_leagues() {
        let st = state();
        *st.leagues.lock().unwrap() = Some(vec![league("Fresh")]);
        st.set_leagues_if_unset(vec![league("Stale")]);
        assert_eq!(st.leagues.lock().unwrap().as_ref().unwrap()[0].id, "Fresh");
        let empty = state();
        empty.set_leagues_if_unset(vec![league("Dawn")]);
        assert_eq!(empty.league(), "Dawn");
    }

    #[test]
    fn data_is_absent_until_decoded() {
        let st = state();
        assert!(st.data().is_none());
        st.data.set(None).unwrap();
        assert!(st.wait_data().is_none(), "a failed decode unblocks waiters");
    }

    #[test]
    fn updated_sources_keep_unchanged_clients() {
        let mut s = Settings::default();
        let base = Sources::from_settings(&s).unwrap();
        s.league.league = Some("Dawn".into());
        s.trade.widen_pct = 25.0;
        assert!(base.updated(&s).unwrap().is_none(), "unrelated change keeps everything");

        s.trade.poesessid = Some("abc".into());
        let next = base.updated(&s).unwrap().expect("poesessid rebuilds trade");
        assert!(!Arc::ptr_eq(&base.trade, &next.trade));
        assert!(Arc::ptr_eq(&base.ninja, &next.ninja) && Arc::ptr_eq(&base.scout, &next.scout));
        assert_eq!(next.trade.config().poesessid.as_deref(), Some("abc"));

        s.trade.contact = Some("me@example.com".into());
        let ua = next.updated(&s).unwrap().expect("user agent rebuilds all");
        assert!(!Arc::ptr_eq(&next.trade, &ua.trade) && !Arc::ptr_eq(&next.ninja, &ua.ninja));
        assert!(ua.updated(&s).unwrap().is_none());
    }
}
