//! Pure, Tauri-independent pieces of the app flow (unit-tested).
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

#[cfg(test)]
mod tests {
    use super::*;

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
