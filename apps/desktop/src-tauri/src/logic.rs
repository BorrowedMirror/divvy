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

/// Parse clipboard text, match stats and build default filters.
pub fn process_item_text(
    text: &str,
    catalog: Option<&dyn Catalog>,
    opts: &QueryOptions,
) -> Result<(ParsedItem, FilterSet), IpcError> {
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

#[cfg(test)]
mod tests {
    use super::*;

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
