use std::{collections::HashMap, sync::Arc};

use kalandra_core::{
    catalog::{BaseDef, Catalog, StatDef, StaticDef, UniqueDef},
    item::ModKind,
};

use crate::{Bundle, Manifest};

/// Trade stat kinds (`StatDef::kind`, the trade2 stats group id) searched for a modifier kind.
///
/// trade2 has no dedicated group for sanctified modifiers; they are searched as explicits.
pub(crate) fn stat_kinds(kind: ModKind) -> &'static [&'static str] {
    match kind {
        ModKind::Implicit => &["implicit"],
        ModKind::Explicit | ModKind::Sanctified => &["explicit"],
        ModKind::Enchant => &["enchant"],
        ModKind::Rune => &["rune"],
        ModKind::Crafted => &["crafted"],
        ModKind::Fractured => &["fractured"],
        ModKind::Desecrated => &["desecrated"],
        ModKind::Skill => &["skill"],
    }
}

/// Kinds searched only when [`stat_kinds`] finds nothing: relic (and sanctum) modifiers
/// print as plain explicits/implicits but trade2 files them under `sanctum`.
pub(crate) fn fallback_stat_kinds(kind: ModKind) -> &'static [&'static str] {
    match kind {
        ModKind::Implicit | ModKind::Explicit | ModKind::Sanctified => &["sanctum"],
        _ => &[],
    }
}

/// kind -> template -> [(stat index, negate)]
type StatIndex = HashMap<String, HashMap<String, Vec<(usize, bool)>>>;

#[derive(Debug)]
struct Indexed {
    bundle: Bundle,
    stats: StatIndex,
    bases: HashMap<String, usize>,
    uniques: HashMap<String, usize>,
    statics: HashMap<String, usize>,
}

impl Indexed {
    fn new(bundle: Bundle) -> Self {
        let mut stats: StatIndex = HashMap::new();
        let mut add = |kind: &str, template: &str, i: usize, negate: bool| {
            let hits = stats.entry(kind.to_owned()).or_default().entry(template.to_owned()).or_default();
            if !hits.iter().any(|(j, _)| *j == i) {
                hits.push((i, negate));
            }
        };
        // Real templates first, then aliases, so direct hits always come first.
        for (i, stat) in bundle.stats.iter().enumerate() {
            add(&stat.kind, &stat.text, i, false);
        }
        for (i, stat) in bundle.stats.iter().enumerate() {
            for alias in &stat.aliases {
                add(&stat.kind, &alias.text, i, alias.negate);
            }
        }
        Self {
            stats,
            bases: first_index(bundle.bases.iter().map(|b| b.name.as_str())),
            uniques: first_index(bundle.uniques.iter().map(|u| u.name.as_str())),
            statics: first_index(bundle.statics.iter().map(|s| s.text.as_str())),
            bundle,
        }
    }
}

/// Map each key to the index of its first occurrence.
fn first_index<'a>(keys: impl Iterator<Item = &'a str>) -> HashMap<String, usize> {
    let mut map = HashMap::new();
    for (i, key) in keys.enumerate() {
        map.entry(key.to_owned()).or_insert(i);
    }
    map
}

/// Indexed, query-ready game data implementing [`Catalog`].
///
/// Cloning is cheap: the bundle and its indexes are shared.
#[derive(Debug, Clone)]
pub struct GameData {
    pub manifest: Manifest,
    inner: Arc<Indexed>,
}

impl GameData {
    pub fn from_bundle(manifest: Manifest, bundle: Bundle) -> Self {
        Self { manifest, inner: Arc::new(Indexed::new(bundle)) }
    }

    pub fn bundle(&self) -> &Bundle {
        &self.inner.bundle
    }

    pub fn manifest(&self) -> &Manifest {
        &self.manifest
    }
}

impl Catalog for GameData {
    fn stats_by_template(&self, template: &str, kind: ModKind) -> Vec<(&StatDef, bool)> {
        let lookup = |kinds: &[&str]| -> Vec<(&StatDef, bool)> {
            kinds
                .iter()
                .filter_map(|k| self.inner.stats.get(*k)?.get(template))
                .flatten()
                .map(|&(i, negate)| (&self.inner.bundle.stats[i], negate))
                .collect()
        };
        let hits = lookup(stat_kinds(kind));
        if hits.is_empty() { lookup(fallback_stat_kinds(kind)) } else { hits }
    }

    fn base(&self, name: &str) -> Option<&BaseDef> {
        self.inner.bases.get(name).map(|&i| &self.inner.bundle.bases[i])
    }

    /// Longest base name that appears as a whole-word run inside `full_name`
    /// (e.g. `Hunter Hood` in `Sturdy Hunter Hood of the Fox`).
    fn base_in_magic_name(&self, full_name: &str) -> Option<&BaseDef> {
        let words: Vec<&str> = full_name.split_whitespace().collect();
        let mut best: Option<&BaseDef> = None;
        for start in 0..words.len() {
            for end in (start + 1)..=words.len() {
                let candidate = words[start..end].join(" ");
                if best.is_some_and(|b| b.name.len() >= candidate.len()) {
                    continue;
                }
                if let Some(base) = self.base(&candidate) {
                    best = Some(base);
                }
            }
        }
        best
    }

    fn unique(&self, name: &str) -> Option<&UniqueDef> {
        self.inner.uniques.get(name).map(|&i| &self.inner.bundle.uniques[i])
    }

    fn static_item(&self, text: &str) -> Option<&StaticDef> {
        self.inner.statics.get(text).map(|&i| &self.inner.bundle.statics[i])
    }
}
