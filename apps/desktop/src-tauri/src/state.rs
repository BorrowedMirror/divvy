//! Long-lived services shared by commands, the hotkey loop, the tray and the test hook.
use std::{
    collections::VecDeque,
    sync::{
        Arc, Mutex, RwLock,
        atomic::{AtomicBool, Ordering},
    },
};

use kalandra_config::{Paths, Settings};
use kalandra_core::{ParsedItem, pricing::PriceSummary, query::FilterSet};
use kalandra_data::GameData;
use kalandra_platform::{BackendKind, Capabilities, Platform};
use kalandra_trade::{
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

impl Sources {
    pub fn from_settings(settings: &Settings) -> Result<Self, String> {
        let ua = logic::user_agent(settings);
        let config = TradeConfig {
            base_url: env_url("KALANDRA_TRADE_BASE_URL", "https://www.pathofexile.com"),
            user_agent: ua.clone(),
            poesessid: settings.trade.poesessid.clone().filter(|s| !s.is_empty()),
            ..TradeConfig::default()
        };
        let trade = TradeClient::new(config, Box::new(SystemClock)).map_err(|e| e.to_string())?;
        Ok(Self {
            trade: Arc::new(trade),
            ninja: Arc::new(PoeNinjaSource::new(env_url("KALANDRA_NINJA_BASE_URL", "https://poe.ninja"), ua.clone())),
            scout: Arc::new(Poe2ScoutSource::new(env_url("KALANDRA_SCOUT_BASE_URL", "https://poe2scout.com"), ua)),
        })
    }

    /// The configured source if it supports the item, else the official trade API.
    pub fn pick(&self, kind: kalandra_core::pricing::PriceSourceKind, item: &ParsedItem) -> Arc<dyn PriceSource> {
        use kalandra_core::pricing::PriceSourceKind as K;
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
    pub data: Option<Arc<GameData>>,
    pub sources: RwLock<Arc<Sources>>,
    pub platform: Option<Arc<Platform>>,
    pub backend: BackendKind,
    pub last_item: Mutex<Option<(ParsedItem, FilterSet)>>,
    pub last_price: Mutex<Option<PriceSummary>>,
    pub leagues: Mutex<Option<Vec<League>>>,
    pub errors: Mutex<VecDeque<String>>,
    pub pinned: AtomicBool,
    pub overlay_visible: AtomicBool,
    pub quitting: AtomicBool,
    pub log_path: Option<std::path::PathBuf>,
}

impl AppState {
    pub fn new(
        paths: Option<Paths>,
        settings: Settings,
        data: Option<Arc<GameData>>,
        platform: Option<Arc<Platform>>,
        backend: BackendKind,
        log_path: Option<std::path::PathBuf>,
    ) -> Result<Self, String> {
        let sources = Sources::from_settings(&settings)?;
        Ok(Self {
            paths,
            settings: RwLock::new(settings),
            data,
            sources: RwLock::new(Arc::new(sources)),
            platform,
            backend,
            last_item: Mutex::new(None),
            last_price: Mutex::new(None),
            leagues: Mutex::new(None),
            errors: Mutex::new(VecDeque::new()),
            pinned: AtomicBool::new(false),
            overlay_visible: AtomicBool::new(false),
            quitting: AtomicBool::new(false),
            log_path,
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

    pub fn set_pinned(&self, pinned: bool) {
        self.pinned.store(pinned, Ordering::SeqCst);
    }

    pub fn is_pinned(&self) -> bool {
        self.pinned.load(Ordering::SeqCst)
    }
}
