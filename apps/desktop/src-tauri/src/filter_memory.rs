//! Remembered stat choices per item category: which stats the user had on or off the last
//! time they searched an item of that category (rings, bows, ...). Stored as JSON in the cache
//! directory; losing it only loses the preference.
use std::collections::BTreeMap;
use std::path::Path;

use rkl_core::query::FilterSet;
use serde::{Deserialize, Serialize};

/// Most stat choices kept per category, so the file stays small.
pub const MAX_PER_CATEGORY: usize = 300;
pub const FILE_NAME: &str = "filter-memory.json";

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct FilterMemory {
    /// category -> stat id -> enabled.
    pub categories: BTreeMap<String, BTreeMap<String, bool>>,
}

fn category(filters: &FilterSet) -> Option<&str> {
    // Uniques are searched by name; their stats start off on purpose.
    if filters.name.is_some() {
        return None;
    }
    filters.category.as_deref().filter(|c| !c.is_empty())
}

impl FilterMemory {
    /// Read the file, or start empty when it is missing or unreadable.
    pub fn load(path: &Path) -> Self {
        std::fs::read(path).ok().and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default()
    }

    /// Write atomically (temp file + rename).
    pub fn save(&self, path: &Path) -> std::io::Result<()> {
        if let Some(dir) = path.parent() {
            std::fs::create_dir_all(dir)?;
        }
        let tmp = path.with_extension("json.tmp");
        std::fs::write(&tmp, serde_json::to_vec(self).map_err(std::io::Error::other)?)?;
        std::fs::rename(tmp, path)
    }

    /// Remember the on/off state of every stat in a search the user ran.
    pub fn record(&mut self, filters: &FilterSet) -> bool {
        let Some(cat) = category(filters) else { return false };
        let entry = self.categories.entry(cat.to_owned()).or_default();
        let mut changed = false;
        for s in &filters.stats {
            if entry.len() >= MAX_PER_CATEGORY && !entry.contains_key(&s.stat_id) {
                continue;
            }
            if entry.insert(s.stat_id.clone(), s.enabled) != Some(s.enabled) {
                changed = true;
            }
        }
        changed
    }

    /// Turn stats on or off as the user last had them for this category.
    pub fn apply(&self, filters: &mut FilterSet) {
        let Some(prefs) = category(filters).and_then(|c| self.categories.get(c)) else { return };
        for s in &mut filters.stats {
            if let Some(&on) = prefs.get(&s.stat_id) {
                s.enabled = on;
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rkl_core::query::{FilterSet, StatFilter};

    fn filters(category: &str, stats: &[(&str, bool)]) -> FilterSet {
        let mut f: FilterSet = serde_json::from_value(serde_json::json!({
            "league": "Standard", "status": "online", "name": null, "base_type": "Ruby Ring",
            "category": category, "rarity": "nonunique", "stats": [], "misc": {
                "item_level": {"enabled": false, "min": null, "max": null},
                "quality": {"enabled": false, "min": null, "max": null},
                "gem_level": {"enabled": false, "min": null, "max": null},
                "waystone_tier": {"enabled": false, "min": null, "max": null},
                "sockets": {"enabled": false, "min": null, "max": null},
                "corrupted": null, "identified": null
            },
            "widen_pct": 10.0, "route": "search"
        }))
        .expect("filter set json");
        f.stats = stats
            .iter()
            .map(|(id, on)| StatFilter {
                stat_id: (*id).into(),
                text: (*id).into(),
                enabled: *on,
                min: None,
                max: None,
                item_values: vec![],
                tiers: vec![],
            })
            .collect();
        f
    }

    #[test]
    fn records_and_applies_per_category_and_round_trips() {
        let mut mem = FilterMemory::default();
        assert!(mem.record(&filters("accessory.ring", &[("a", false), ("b", true)])));
        assert!(!mem.record(&filters("accessory.ring", &[("a", false), ("b", true)])), "no change");
        let mut next = filters("accessory.ring", &[("a", true), ("b", false), ("c", true)]);
        mem.apply(&mut next);
        let on: Vec<bool> = next.stats.iter().map(|s| s.enabled).collect();
        assert_eq!(on, [false, true, true], "unknown stats keep their default");
        let mut other = filters("weapon.bow", &[("a", true)]);
        mem.apply(&mut other);
        assert!(other.stats[0].enabled, "other categories are untouched");

        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("sub").join(FILE_NAME);
        mem.save(&path).unwrap();
        assert_eq!(FilterMemory::load(&path), mem);
        std::fs::write(&path, b"not json").unwrap();
        assert_eq!(FilterMemory::load(&path), FilterMemory::default());
    }

    #[test]
    fn uniques_are_not_remembered_and_the_size_is_capped() {
        let mut mem = FilterMemory::default();
        let mut unique = filters("accessory.ring", &[("a", true)]);
        unique.name = Some("Andvarius".into());
        assert!(!mem.record(&unique));
        let many: Vec<(String, bool)> = (0..MAX_PER_CATEGORY + 5).map(|i| (format!("s{i}"), true)).collect();
        let refs: Vec<(&str, bool)> = many.iter().map(|(s, b)| (s.as_str(), *b)).collect();
        mem.record(&filters("accessory.ring", &refs));
        assert_eq!(mem.categories["accessory.ring"].len(), MAX_PER_CATEGORY);
    }
}
