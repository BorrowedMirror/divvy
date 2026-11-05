//! Matches corpus items against the real bundled trade2 catalog from `kalandra-data`.
use std::path::PathBuf;

use kalandra_core::{
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
    let data = kalandra_data::bundled().expect("bundled data");
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
    let data = kalandra_data::bundled().expect("bundled data");
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
