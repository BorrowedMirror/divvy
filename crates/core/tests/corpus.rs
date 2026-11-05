//! Parses every corpus item and snapshots the result; snapshots trade queries for a subset.
//!
//! The corpus is read straight from `crates/testkit/corpus` (rather than via a
//! `kalandra-testkit` dev-dependency) to avoid two copies of `kalandra-core` types.
use std::{collections::HashMap, path::PathBuf};

use kalandra_core::{
    catalog::{BaseDef, Catalog, StatDef, StaticDef, UniqueDef},
    item::{ModKind, ParsedItem},
    matcher::StatMatcher,
    parser::parse_item,
    query::{QueryOptions, build_filters},
};
use serde::Deserialize;

#[derive(Debug, Deserialize)]
struct Entry {
    id: String,
    class: String,
    rarity: String,
    #[serde(default)]
    advanced: bool,
}

#[derive(Debug, Deserialize)]
struct Index {
    item: Vec<Entry>,
}

fn corpus_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testkit/corpus")
}

fn load() -> Vec<(Entry, String)> {
    let dir = corpus_dir();
    let index: Index = toml::from_str(&std::fs::read_to_string(dir.join("index.toml")).unwrap()).unwrap();
    index
        .item
        .into_iter()
        .map(|e| {
            let text = std::fs::read_to_string(dir.join(format!("{}.txt", e.id))).unwrap();
            (e, text)
        })
        .collect()
}

fn snapshot_name(id: &str) -> String {
    id.replace('/', "__")
}

fn kind_name(kind: ModKind) -> &'static str {
    match kind {
        ModKind::Implicit => "implicit",
        ModKind::Explicit | ModKind::Sanctified => "explicit",
        ModKind::Enchant => "enchant",
        ModKind::Rune => "rune",
        ModKind::Crafted => "crafted",
        ModKind::Fractured => "fractured",
        ModKind::Desecrated => "desecrated",
        ModKind::Skill => "skill",
    }
}

/// Catalog that knows every template seen in the corpus, with stable synthetic ids.
#[derive(Debug, Default)]
struct CorpusCatalog {
    stats: HashMap<(String, String), StatDef>,
}

fn fnv(s: &str) -> u32 {
    s.bytes().fold(0x811c_9dc5u32, |h, b| (h ^ u32::from(b)).wrapping_mul(0x0100_0193))
}

impl CorpusCatalog {
    fn from_items(items: &[ParsedItem]) -> Self {
        let mut stats = HashMap::new();
        for m in items.iter().flat_map(|i| &i.modifiers) {
            let kind = kind_name(m.kind).to_owned();
            let id = format!("{kind}.stat_{}", fnv(&m.template));
            stats.insert(
                (kind.clone(), m.template.clone()),
                StatDef { id, text: m.template.clone(), kind, aliases: vec![] },
            );
        }
        Self { stats }
    }
}

impl Catalog for CorpusCatalog {
    fn stats_by_template(&self, template: &str, kind: ModKind) -> Vec<(&StatDef, bool)> {
        self.stats.get(&(kind_name(kind).to_owned(), template.to_owned())).map(|s| (s, false)).into_iter().collect()
    }
    fn base(&self, _name: &str) -> Option<&BaseDef> {
        None
    }
    fn base_in_magic_name(&self, _full_name: &str) -> Option<&BaseDef> {
        None
    }
    fn unique(&self, _name: &str) -> Option<&UniqueDef> {
        None
    }
    fn static_item(&self, _text: &str) -> Option<&StaticDef> {
        None
    }
}

fn variant_name(item: &ParsedItem) -> String {
    let dbg = format!("{:?}", item.class);
    dbg.split('(').next().unwrap().to_owned()
}

#[test]
fn corpus_is_large_and_covers_advanced_and_crlf() {
    let items = load();
    assert!(items.len() >= 60, "corpus has {} items", items.len());
    assert!(items.iter().filter(|(e, _)| e.advanced).count() >= 10);
    assert!(items.iter().any(|(_, t)| t.contains("\r\n")));
}

#[test]
fn corpus_parses_and_matches_index() {
    for (entry, text) in load() {
        let item = parse_item(&text).unwrap_or_else(|e| panic!("{}: {e}", entry.id));
        assert_eq!(variant_name(&item), entry.class, "{} class", entry.id);
        let rarity = serde_json::to_value(item.rarity).unwrap();
        assert_eq!(rarity, entry.rarity, "{} rarity", entry.id);
        assert_eq!(item.advanced, entry.advanced, "{} advanced", entry.id);
        insta::with_settings!({ snapshot_path => "snapshots/items", prepend_module_to_snapshot => false }, {
            insta::assert_yaml_snapshot!(snapshot_name(&entry.id), item, { ".raw" => "[raw]" });
        });
    }
}

#[test]
fn trade_queries_for_representative_items() {
    let ids = [
        "currency/divine-orb",
        "gem/lightning-arrow-corrupted",
        "waystone/rare-tier-15",
        "jewel/rare-ruby",
        "ring/rare-sapphire-ring-advanced",
        "helmet/unique-goldrim",
        "body-armour/rare-vile-robe",
        "weapon/rare-recurve-bow",
        "weapon/unidentified-rare-crossbow",
        "gloves/magic-gloves",
    ];
    let all: Vec<_> = load();
    let parsed: Vec<ParsedItem> = all.iter().map(|(_, t)| parse_item(t).unwrap()).collect();
    let catalog = CorpusCatalog::from_items(&parsed);
    let matcher = StatMatcher::new(&catalog);
    let opts = QueryOptions { league: "Standard".into(), online_only: true, widen_pct: 10.0, enable_all_stats: false };
    for id in ids {
        let idx = all.iter().position(|(e, _)| e.id == id).unwrap_or_else(|| panic!("missing {id}"));
        let mut item = parsed[idx].clone();
        matcher.match_item(&mut item);
        let filters = build_filters(&item, &opts);
        let query = filters.to_trade_query();
        insta::with_settings!({ snapshot_path => "snapshots/queries", prepend_module_to_snapshot => false }, {
            insta::assert_json_snapshot!(snapshot_name(id), query);
        });
    }
}
