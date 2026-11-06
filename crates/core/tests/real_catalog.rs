//! Matches corpus items against the real bundled trade2 catalog from `rkl-data`.
use std::path::PathBuf;

use rkl_core::{
    item::{ModKind, ParsedItem},
    matcher::StatMatcher,
    parser::parse_item,
};
use serde::Deserialize;

#[derive(Debug, Deserialize)]
struct Entry {
    id: String,
}

#[derive(Debug, Deserialize)]
struct Index {
    item: Vec<Entry>,
}

fn corpus() -> Vec<(String, ParsedItem)> {
    let dir = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testkit/corpus");
    let index: Index = toml::from_str(&std::fs::read_to_string(dir.join("index.toml")).unwrap()).unwrap();
    index
        .item
        .into_iter()
        .map(|e| {
            let text = std::fs::read_to_string(dir.join(format!("{}.txt", e.id))).unwrap();
            let item = parse_item(&text).unwrap();
            (e.id, item)
        })
        .collect()
}

fn matched(id: &str) -> ParsedItem {
    let data = rkl_data::bundled().expect("bundled data");
    let (_, mut item) = corpus().into_iter().find(|(i, _)| i == id).unwrap();
    StatMatcher::new(&data).match_item(&mut item);
    item
}

fn stat_of<'a>(item: &'a ParsedItem, text: &str) -> Option<&'a str> {
    item.modifiers.iter().find(|m| m.text == text).and_then(|m| m.stat_id.as_deref())
}

#[test]
fn local_armour_on_gloves() {
    let gloves = matched("gloves/rare-gloves-advanced");
    assert_eq!(stat_of(&gloves, "+98 to Armour"), Some("explicit.stat_3484657501"));
}

#[test]
fn local_evasion_on_helmet() {
    let helmet = matched("helmet/rare-hunter-hood-advanced");
    assert_eq!(stat_of(&helmet, "+109 to Evasion Rating"), Some("explicit.stat_53045048"));
}

#[test]
fn bow_additional_arrow_matches_plural_stat() {
    let bow = matched("weapon/rare-bow-advanced");
    let m = bow.modifiers.iter().find(|m| m.text == "Bow Attacks fire an additional Arrow").unwrap();
    assert!(m.stat_id.is_some(), "additional arrow unmatched");
    assert_eq!(m.values, vec![1.0]);
}

#[test]
fn corpus_match_rate_floor() {
    let data = rkl_data::bundled().expect("bundled data");
    let matcher = StatMatcher::new(&data);
    let (mut total, mut hits) = (0usize, 0usize);
    let mut misses = Vec::new();
    for (id, mut item) in corpus() {
        matcher.match_item(&mut item);
        for m in item.modifiers.iter().filter(|m| matches!(m.kind, ModKind::Explicit | ModKind::Implicit)) {
            total += 1;
            if m.stat_id.is_some() {
                hits += 1;
            } else {
                misses.push(format!("{id}: {}", m.text));
            }
        }
    }
    let rate = hits as f64 / total as f64;
    eprintln!("match rate {hits}/{total} = {:.1}%\nunmatched:\n{}", rate * 100.0, misses.join("\n"));
    assert!(rate >= 0.85, "match rate {:.1}% below 85%", rate * 100.0);
}

/// Real user report: a sceptre search returned nothing. Spirit must use the sceptre's local
/// stat, the prefix and desecrated Spirit mods must be one summed filter, desecrated mods are
/// searched as explicit, and "reduced" requirements get an upper bound.
#[test]
fn sceptre_spirit_search_matches_trade_listings() {
    use rkl_core::query::{QueryOptions, build_filters};
    let sceptre = matched("weapon/rare-sceptre-spirit-advanced");
    assert_eq!(stat_of(&sceptre, "55% increased Spirit"), Some("explicit.stat_3984865854"));
    assert_eq!(stat_of(&sceptre, "13% increased Spirit"), Some("desecrated.stat_3984865854"));
    let opts = QueryOptions {
        league: "Standard".into(),
        status: rkl_core::query::OnlineStatus::Online,
        widen_pct: 10.0,
        enable_all_stats: false,
    };
    let f = build_filters(&sceptre, &opts);
    let spirit: Vec<_> = f.stats.iter().filter(|s| s.stat_id.ends_with("stat_3984865854")).collect();
    assert_eq!(spirit.len(), 1, "one summed Spirit filter: {:?}", f.stats);
    assert_eq!(spirit[0].stat_id, "explicit.stat_3984865854");
    assert_eq!(spirit[0].item_values, vec![68.0]);
    assert_eq!(spirit[0].min, Some(61.0));
    assert_eq!(spirit[0].text, "68% increased Spirit");
    assert!(f.stats.iter().all(|s| !s.stat_id.starts_with("desecrated.")), "{:?}", f.stats);
    let reqs = f.stats.iter().find(|s| s.text == "30% reduced Attribute Requirements").unwrap();
    assert_eq!((reqs.min, reqs.max), (None, Some(-27.0)));
}

#[test]
fn global_spirit_on_amulets_stays_global() {
    use rkl_core::{catalog::Catalog, item::ModKind};
    let data = rkl_data::bundled().expect("bundled data");
    let ids: Vec<_> =
        data.stats_by_template("#% increased Spirit", ModKind::Explicit).iter().map(|(s, _)| s.id.clone()).collect();
    assert!(
        ids.contains(&"explicit.stat_1416406066".to_owned()) && ids.contains(&"explicit.stat_3984865854".to_owned()),
        "{ids:?}"
    );
    let text = "Item Class: Amulets\nRarity: Rare\nGale Charm\nGold Amulet\n--------\nItem Level: 80\n--------\n20% increased Spirit\n";
    let mut amulet = rkl_core::parser::parse_item(text).unwrap();
    StatMatcher::new(&data).match_item(&mut amulet);
    assert_eq!(stat_of(&amulet, "20% increased Spirit"), Some("explicit.stat_1416406066"));
}

#[test]
fn stat_filters_carry_mod_tiers_from_advanced_copies() {
    use rkl_core::query::{OnlineStatus, QueryOptions, build_filters};
    let sceptre = matched("weapon/rare-sceptre-spirit-advanced");
    let opts =
        QueryOptions { league: "Standard".into(), status: OnlineStatus::Any, widen_pct: 10.0, enable_all_stats: false };
    let f = build_filters(&sceptre, &opts);
    let tier = |text: &str| f.stats.iter().find(|s| s.text == text).map(|s| s.tier);
    assert_eq!(tier("30% reduced Attribute Requirements"), Some(Some(2)));
    assert_eq!(tier("Allies in your Presence Regenerate 2.2 Life per second"), Some(Some(9)));
    assert_eq!(tier("68% increased Spirit"), Some(None), "summed prefix + desecrated row");
}

/// The waystone mod check keys flags by template: the templates the parser produces for real
/// waystones must be exactly the strings flagged by default.
#[test]
fn waystone_templates_match_the_default_danger_flags() {
    let templates: Vec<String> = ["waystone/corrupted-tier-16-advanced", "waystone/rare-tier-15"]
        .iter()
        .flat_map(|id| matched(id).modifiers.into_iter().map(|m| m.template))
        .collect();
    for danger in [
        "Players have -#% to all maximum Resistances",
        "Players have #% less Recovery Rate of Life and Energy Shield",
        "Monsters fire # additional Projectiles",
    ] {
        assert!(templates.iter().any(|t| t == danger), "{danger:?} not among {templates:#?}");
    }
}
