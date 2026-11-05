//! Pure, Tauri-independent pieces of the app flow (unit-tested).
use std::{
    sync::{
        Arc, Mutex, PoisonError,
        atomic::{AtomicBool, Ordering},
    },
    time::{Duration, Instant},
};

use kalandra_config::Settings;
use kalandra_core::{
    ParsedItem,
    catalog::Catalog,
    matcher::StatMatcher,
    parser::parse_item,
    query::{FilterSet, QueryOptions, build_filters},
};
use kalandra_trade::types::League;

use crate::ipc::IpcError;

/// Largest clipboard text accepted as an item (real items are a few KiB).
pub const MAX_ITEM_TEXT: usize = 64 * 1024;

/// Parse clipboard text, match stats and build default filters.
pub fn process_item_text(
    text: &str,
    catalog: Option<&dyn Catalog>,
    opts: &QueryOptions,
) -> Result<(ParsedItem, FilterSet), IpcError> {
    if text.len() > MAX_ITEM_TEXT {
        return Err(IpcError::new("parse", "item text too large"));
    }
    let mut item = parse_item(text).map_err(|e| IpcError::new("parse", e.to_string()))?;
    if let Some(catalog) = catalog {
        StatMatcher::new(catalog).match_item(&mut item);
    }
    let filters = build_filters(&item, opts);
    Ok((item, filters))
}

/// Query options derived from settings and the resolved league.
pub fn query_options(settings: &Settings, league: &str) -> QueryOptions {
    QueryOptions {
        league: league.to_owned(),
        online_only: settings.trade.online_only,
        widen_pct: settings.trade.widen_pct,
        enable_all_stats: settings.trade.enable_all_stats,
    }
}

/// Pick the current softcore trade league: the first league that is not Standard,
/// Hardcore, SSF, Solo Self-Found or a private/event ruthless variant; falls back to `Standard`.
pub fn auto_league(leagues: &[League]) -> Option<String> {
    let is_challenge_sc = |l: &&League| {
        let id = l.id.to_ascii_lowercase();
        !(id == "standard"
            || id.contains("hardcore")
            || id.starts_with("hc ")
            || id.contains("ssf")
            || id.contains("solo self-found")
            || id.contains("ruthless"))
    };
    leagues
        .iter()
        .find(is_challenge_sc)
        .or_else(|| leagues.iter().find(|l| l.id.eq_ignore_ascii_case("standard")))
        .map(|l| l.id.clone())
}

/// League to use given settings and (optionally) the live list.
pub fn resolve_league(settings: &Settings, live: Option<&[League]>) -> String {
    if !settings.league.auto_detect
        && let Some(l) = settings.league.league.as_deref().filter(|l| !l.is_empty())
    {
        return l.to_owned();
    }
    live.and_then(auto_league).or_else(|| settings.league.league.clone()).unwrap_or_else(|| "Standard".to_owned())
}

/// Integer rectangle in physical pixels.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Area {
    pub x: i32,
    pub y: i32,
    pub width: i32,
    pub height: i32,
}

/// Place a panel of `size` beside `cursor`, flipping to the left of the cursor when it would
/// overflow the right edge of `screen`, and clamping vertically.
pub fn overlay_position(cursor: (i32, i32), size: (i32, i32), screen: Area) -> (i32, i32) {
    const GAP: i32 = 24;
    let (cx, cy) = cursor;
    let (w, h) = size;
    let right = screen.x + screen.width;
    let bottom = screen.y + screen.height;
    let x = if cx + GAP + w <= right { cx + GAP } else { (cx - GAP - w).max(screen.x) };
    let y = (cy - h / 2).clamp(screen.y, (bottom - h).max(screen.y));
    (x, y)
}

/// User-Agent including the optional contact from settings.
pub fn user_agent(settings: &Settings) -> String {
    let base = kalandra_trade::default_user_agent();
    match settings.trade.contact.as_deref().filter(|c| !c.is_empty()) {
        Some(c) => format!("{base} {c}"),
        None => base,
    }
}

/// Tracing filter: `KALANDRA_LOG` (or `info`), with the HTTP/TLS stack capped at `info` so
/// debug logs never contain request headers (POESESSID cookie).
pub fn log_filter(env: Option<&str>) -> String {
    let base = env.map(str::trim).filter(|s| !s.is_empty()).unwrap_or("info");
    let caps = ["reqwest", "hyper", "hyper_util", "h2", "rustls"].map(|t| format!("{t}=info")).join(",");
    format!("{base},{caps}")
}

/// Whether an env URL override is acceptable: https anywhere, or http on loopback only.
pub fn is_safe_override(url: &url::Url) -> bool {
    match url.scheme() {
        "https" => url.host().is_some(),
        "http" => match url.host() {
            Some(url::Host::Domain(d)) => d.eq_ignore_ascii_case("localhost"),
            Some(url::Host::Ipv4(ip)) => ip.is_loopback(),
            Some(url::Host::Ipv6(ip)) => ip.is_loopback(),
            None => false,
        },
        _ => false,
    }
}

/// Only the official trade site's search pages, or (debug/test builds) a vetted override origin.
pub fn is_allowed_trade_url(url: &str, override_base: Option<&url::Url>) -> bool {
    let Ok(parsed) = url::Url::parse(url) else { return false };
    let trade_path = parsed.path().starts_with("/trade2/") || parsed.path().starts_with("/trade/");
    match parsed.host_str() {
        Some("www.pathofexile.com" | "pathofexile.com") => {
            parsed.scheme() == "https" && parsed.port().is_none() && trade_path
        }
        Some(_) => override_base.is_some_and(|o| is_safe_override(o) && o.origin() == parsed.origin() && trade_path),
        None => false,
    }
}

/// Whether the copy keystroke may be sent: only into the focused game window when the backend
/// can track windows (otherwise e.g. a terminal would receive Ctrl+C).
pub fn may_copy(window_tracking: bool, game_focused: impl FnOnce() -> bool) -> bool {
    !window_tracking || game_focused()
}

/// Whether the shell must handle SIGUSR2 itself: a Wayland platform already maps it to a hotkey.
/// SIGUSR1 (pinned) is always the shell's, else its default action would kill the app.
pub fn shell_handles_sigusr2(wayland_platform: bool) -> bool {
    !wayland_platform
}

/// Serialises price-check flows: one copy at a time, and presses within the debounce window of
/// the last accepted one are dropped (key repeat / storms). Overlapping copies could otherwise
/// "restore" the clipboard to the item text captured by the other run.
#[derive(Debug)]
pub struct FlowGate {
    in_flight: AtomicBool,
    last: Mutex<Option<Instant>>,
    debounce: Duration,
}

/// Held while a flow runs; dropping it lets the next press through.
#[derive(Debug)]
pub struct FlowPermit(Arc<FlowGate>);

impl Drop for FlowPermit {
    fn drop(&mut self) {
        self.0.in_flight.store(false, Ordering::SeqCst);
    }
}

impl FlowGate {
    pub const DEBOUNCE: Duration = Duration::from_millis(150);

    pub fn new(debounce: Duration) -> Self {
        Self { in_flight: AtomicBool::new(false), last: Mutex::new(None), debounce }
    }

    /// Start a flow at `now`, or `None` if one is running or the last start was too recent.
    pub fn try_begin(self: &Arc<Self>, now: Instant) -> Option<FlowPermit> {
        let mut last = self.last.lock().unwrap_or_else(PoisonError::into_inner);
        if last.is_some_and(|t| now.saturating_duration_since(t) < self.debounce) {
            return None;
        }
        if self.in_flight.compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst).is_err() {
            return None;
        }
        *last = Some(now);
        Some(FlowPermit(Arc::clone(self)))
    }
}

impl Default for FlowGate {
    fn default() -> Self {
        Self::new(Self::DEBOUNCE)
    }
}

/// Show settings at startup only when the user would otherwise have no way in: neither a tray
/// icon nor a registered hotkey. With either one the app is reachable (missing hotkeys are
/// explained by first-run onboarding and the diagnostics page, and `kalandra --trigger` works).
pub fn must_show_settings(tray_ok: bool, hotkeys_ok: bool) -> bool {
    !tray_ok && !hotkeys_ok
}

/// Smallest overlay height we allow (logical px), so the header and empty state always fit.
pub const MIN_OVERLAY_HEIGHT: f64 = 120.0;
/// Gap kept between the overlay and the top/bottom of the monitor (logical px).
pub const OVERLAY_SCREEN_MARGIN: f64 = 8.0;

/// Height (logical px) to give the overlay for content of `wanted` px on a monitor that is
/// `monitor_height` logical px tall: as tall as the content, never taller than the screen.
pub fn fit_overlay_height(wanted: f64, monitor_height: f64) -> f64 {
    let max = (monitor_height - 2.0 * OVERLAY_SCREEN_MARGIN).max(MIN_OVERLAY_HEIGHT);
    if wanted.is_finite() { wanted.ceil().clamp(MIN_OVERLAY_HEIGHT, max) } else { MIN_OVERLAY_HEIGHT }
}

/// Top edge (physical px) that keeps a window of `height` starting at `y` inside the monitor
/// spanning `screen_y .. screen_y + screen_height`.
pub fn keep_on_screen_y(y: i32, height: i32, screen_y: i32, screen_height: i32) -> i32 {
    let bottom = screen_y + screen_height;
    if y + height > bottom { (bottom - height).max(screen_y) } else { y.max(screen_y) }
}

/// What a global hotkey press does. Every hotkey toggles: pressing it while its window is open
/// closes it (like Esc), otherwise it opens it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HotkeyEffect {
    PriceCheck { pinned: bool },
    HideOverlay,
    ShowSettings,
    HideSettings,
}

pub fn hotkey_effect(action: kalandra_platform::HotkeyAction, overlay_open: bool, settings_open: bool) -> HotkeyEffect {
    use kalandra_platform::HotkeyAction as A;
    match action {
        A::PriceCheck | A::PriceCheckPinned if overlay_open => HotkeyEffect::HideOverlay,
        A::PriceCheck => HotkeyEffect::PriceCheck { pinned: false },
        A::PriceCheckPinned => HotkeyEffect::PriceCheck { pinned: true },
        A::OpenSettings if settings_open => HotkeyEffect::HideSettings,
        A::OpenSettings => HotkeyEffect::ShowSettings,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hotkeys_toggle_their_window() {
        use kalandra_platform::HotkeyAction as A;
        assert_eq!(hotkey_effect(A::PriceCheck, false, false), HotkeyEffect::PriceCheck { pinned: false });
        assert_eq!(hotkey_effect(A::PriceCheckPinned, false, true), HotkeyEffect::PriceCheck { pinned: true });
        assert_eq!(hotkey_effect(A::PriceCheck, true, false), HotkeyEffect::HideOverlay);
        assert_eq!(hotkey_effect(A::PriceCheckPinned, true, false), HotkeyEffect::HideOverlay);
        assert_eq!(hotkey_effect(A::OpenSettings, true, false), HotkeyEffect::ShowSettings);
        assert_eq!(hotkey_effect(A::OpenSettings, false, true), HotkeyEffect::HideSettings);
    }

    #[test]
    fn overlay_height_fits_content_within_the_screen() {
        assert_eq!(fit_overlay_height(431.2, 1440.0), 432.0);
        assert_eq!(fit_overlay_height(50.0, 1440.0), MIN_OVERLAY_HEIGHT);
        assert_eq!(fit_overlay_height(5000.0, 1440.0), 1440.0 - 2.0 * OVERLAY_SCREEN_MARGIN);
        assert_eq!(fit_overlay_height(f64::NAN, 1440.0), MIN_OVERLAY_HEIGHT);
        assert_eq!(fit_overlay_height(500.0, 100.0), MIN_OVERLAY_HEIGHT);
    }

    #[test]
    fn keeps_window_inside_the_monitor_vertically() {
        assert_eq!(keep_on_screen_y(100, 400, 0, 1440), 100);
        assert_eq!(keep_on_screen_y(1200, 400, 0, 1440), 1040);
        assert_eq!(keep_on_screen_y(-20, 400, 0, 1440), 0);
        assert_eq!(keep_on_screen_y(100, 2000, 0, 1440), 0);
    }

    #[test]
    fn settings_shown_when_unreachable() {
        assert!(!must_show_settings(true, true));
        assert!(!must_show_settings(false, true));
        assert!(!must_show_settings(true, false));
        assert!(must_show_settings(false, false));
    }

    #[test]
    fn flow_gate_serialises_and_debounces() {
        let gate = Arc::new(FlowGate::default());
        let t0 = Instant::now();
        let ms = Duration::from_millis;
        let p = gate.try_begin(t0).expect("first press runs");
        assert!(gate.try_begin(t0 + ms(500)).is_none(), "dropped while in flight");
        drop(p);
        assert!(gate.try_begin(t0 + ms(100)).is_none(), "debounced");
        let p = gate.try_begin(t0 + ms(200)).expect("after debounce");
        drop(p);
        assert!(gate.try_begin(t0).is_none(), "earlier instant is too soon");
    }

    #[test]
    fn copy_requires_focus_only_with_tracking() {
        use kalandra_platform::{Rect, WindowTracker};
        use kalandra_testkit::fakes::{FakeWindowTracker, game_window};
        assert!(may_copy(false, || false));
        let tracker = FakeWindowTracker::new();
        assert!(!may_copy(true, || tracker.is_game_focused()), "no game window");
        tracker.set_window(Some(game_window(Rect { x: 0, y: 0, width: 10, height: 10 })));
        assert!(may_copy(true, || tracker.is_game_focused()));
        tracker.set_focused(false);
        assert!(!may_copy(true, || tracker.is_game_focused()), "terminal focused");
        assert!(may_copy(false, || tracker.is_game_focused()), "no tracking => allow");
    }

    #[test]
    fn sigusr2_ownership() {
        assert!(shell_handles_sigusr2(false));
        assert!(!shell_handles_sigusr2(true));
    }

    #[test]
    fn log_filter_caps_http_stack() {
        assert_eq!(log_filter(None), "info,reqwest=info,hyper=info,hyper_util=info,h2=info,rustls=info");
        let f = log_filter(Some("debug"));
        assert!(f.starts_with("debug,") && f.contains("hyper=info") && f.contains("reqwest=info"));
        assert!(tracing_subscriber::EnvFilter::try_new(&f).is_ok());
    }

    #[test]
    fn override_urls_must_be_https_or_loopback() {
        let ok = |s: &str| is_safe_override(&url::Url::parse(s).unwrap());
        assert!(ok("https://example.com"));
        assert!(ok("http://127.0.0.1:18080"));
        assert!(ok("http://localhost:1"));
        assert!(ok("http://[::1]:1"));
        assert!(!ok("http://example.com"));
        assert!(!ok("http://10.0.0.1"));
        assert!(!ok("file:///etc/passwd"));
    }

    #[test]
    fn trade_url_allow_list() {
        let a = |u: &str| is_allowed_trade_url(u, None);
        assert!(a("https://www.pathofexile.com/trade2/search/poe2/Standard/abc"));
        assert!(a("https://pathofexile.com/trade/search/Standard/abc"));
        assert!(!a("https://www.pathofexile.com/forum/view-thread/1"));
        assert!(!a("https://www.pathofexile.com/trade2"));
        assert!(!a("http://www.pathofexile.com/trade2/search/x"));
        assert!(!a("https://www.pathofexile.com:8443/trade2/search/x"));
        assert!(!a("https://evil.example.com/trade2/search/x"));
        assert!(!a("not a url"));
        let local = url::Url::parse("http://127.0.0.1:18080").unwrap();
        assert!(is_allowed_trade_url("http://127.0.0.1:18080/trade2/search/poe2/x", Some(&local)));
        assert!(!is_allowed_trade_url("http://127.0.0.1:9999/trade2/search/poe2/x", Some(&local)));
        let remote = url::Url::parse("http://evil.example.com").unwrap();
        assert!(!is_allowed_trade_url("http://evil.example.com/trade2/search/x", Some(&remote)));
    }

    #[test]
    fn oversized_item_text_is_rejected() {
        let text = "x".repeat(MAX_ITEM_TEXT + 1);
        let err = process_item_text(&text, None, &query_options(&Settings::default(), "Standard")).unwrap_err();
        assert_eq!((err.kind.as_str(), err.message.as_str()), ("parse", "item text too large"));
    }

    fn league(id: &str) -> League {
        League { id: id.into(), realm: Some("poe2".into()), text: Some(id.into()) }
    }

    #[test]
    fn auto_league_prefers_challenge_softcore() {
        let ls =
            [league("Standard"), league("Hardcore"), league("Rise of the Abyssal"), league("HC Rise of the Abyssal")];
        assert_eq!(auto_league(&ls).as_deref(), Some("Rise of the Abyssal"));
        assert_eq!(auto_league(&[league("Standard"), league("Hardcore")]).as_deref(), Some("Standard"));
        assert_eq!(auto_league(&[]), None);
        assert_eq!(auto_league(&[league("SSF Standard")]), None);
    }

    #[test]
    fn resolve_league_respects_manual_setting() {
        let mut s = Settings::default();
        s.league.auto_detect = false;
        s.league.league = Some("Standard".into());
        assert_eq!(resolve_league(&s, Some(&[league("Dawn")])), "Standard");
        s.league.auto_detect = true;
        assert_eq!(resolve_league(&s, Some(&[league("Dawn")])), "Dawn");
        assert_eq!(resolve_league(&s, None), "Standard");
        s.league.league = None;
        assert_eq!(resolve_league(&s, None), "Standard");
    }

    #[test]
    fn overlay_flips_near_right_edge_and_clamps() {
        let screen = Area { x: 0, y: 0, width: 1920, height: 1080 };
        assert_eq!(overlay_position((100, 500), (420, 560), screen), (124, 220));
        assert_eq!(overlay_position((1800, 500), (420, 560), screen), (1356, 220));
        assert_eq!(overlay_position((1800, 10), (420, 560), screen), (1356, 0));
        assert_eq!(overlay_position((1800, 1070), (420, 560), screen), (1356, 520));
        let tiny = Area { x: 0, y: 0, width: 300, height: 200 };
        assert_eq!(overlay_position((10, 10), (420, 560), tiny), (0, 0));
    }

    #[test]
    fn user_agent_appends_contact() {
        let mut s = Settings::default();
        assert!(!user_agent(&s).ends_with("me@example.com"));
        s.trade.contact = Some("me@example.com".into());
        assert!(user_agent(&s).ends_with("me@example.com"));
    }

    #[test]
    fn query_options_from_settings() {
        let s = Settings::default();
        let o = query_options(&s, "Dawn");
        assert_eq!(o.league, "Dawn");
        assert!(o.online_only);
    }
}
