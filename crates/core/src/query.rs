//! Editable filters and trade2 search query construction.
use serde::{Deserialize, Serialize};

use serde_json::{Map, Value, json};

use crate::item::{ItemCategory, ItemClass, ModKind, ParsedItem, Rarity};

/// Options from settings that influence default filters.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct QueryOptions {
    pub league: String,
    pub online_only: bool,
    /// Default widening applied to stat values, in percent (e.g. 10 => min = value * 0.9).
    pub widen_pct: f64,
    /// Enable all matched explicit stats by default (otherwise only the top few).
    pub enable_all_stats: bool,
}

/// Trade "status" option.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum OnlineStatus {
    /// `securable` (instant buyout) in trade2.
    Securable,
    Online,
    Any,
}

/// One editable stat row in the price panel.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct StatFilter {
    pub stat_id: String,
    /// Display text from the item.
    pub text: String,
    pub enabled: bool,
    pub min: Option<f64>,
    pub max: Option<f64>,
    /// Value(s) on the item, for showing beside the inputs and re-widening.
    pub item_values: Vec<f64>,
}

/// Numeric range filter for misc properties (ilvl, quality, gem level, ...).
#[derive(Debug, Clone, Copy, PartialEq, Default, Serialize, Deserialize, specta::Type)]
pub struct RangeFilter {
    pub enabled: bool,
    pub min: Option<f64>,
    pub max: Option<f64>,
}

#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize, specta::Type)]
pub struct MiscFilters {
    pub item_level: RangeFilter,
    pub quality: RangeFilter,
    pub gem_level: RangeFilter,
    pub waystone_tier: RangeFilter,
    pub sockets: RangeFilter,
    pub corrupted: Option<bool>,
    pub identified: Option<bool>,
}

/// Everything the user can edit before (re)querying. Round-trips through IPC.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct FilterSet {
    pub league: String,
    pub status: OnlineStatus,
    /// Exact name for uniques (`name` in trade2), base type for everything else.
    pub name: Option<String>,
    pub base_type: Option<String>,
    /// Trade category filter, e.g. `armour.helmet`.
    pub category: Option<String>,
    pub rarity: Option<String>,
    pub stats: Vec<StatFilter>,
    pub misc: MiscFilters,
    pub widen_pct: f64,
    /// Route: bulk exchange (currency/fragments) vs item search.
    pub route: QueryRoute,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum QueryRoute {
    /// `/api/trade2/search/{league}`
    Search,
    /// `/api/trade2/exchange/{league}` (bulk currency).
    Exchange,
}

/// Body sent to `/api/trade2/search/{league}`. Serialises to the exact trade2 JSON shape.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct TradeQuery {
    pub query: serde_json::Value,
    pub sort: serde_json::Value,
}

/// Explicit-like stats enabled by default when `enable_all_stats` is off.
pub const DEFAULT_ENABLED_STATS: usize = 4;

/// Lower bound for a stat value `v` widened by `pct` percent (never above `v`).
///
/// Integer item values produce integer bounds (floored); fractional ones keep two decimals.
pub fn widen_min(v: f64, pct: f64, integral: bool) -> f64 {
    let pct = pct.clamp(0.0, 100.0);
    let raw = v - v.abs() * pct / 100.0;
    // Small epsilon so float noise (5.5 * 0.9 = 4.949999...) does not drop a cent.
    let out = if integral { (raw + 1e-9).floor() } else { (raw * 100.0 + 1e-6).floor() / 100.0 };
    out.min(v)
}

fn widen_values(values: &[f64], pct: f64) -> Option<f64> {
    if values.is_empty() {
        return None;
    }
    let avg = values.iter().sum::<f64>() / values.len() as f64;
    let integral = values.iter().all(|v| v.fract() == 0.0) && avg.fract() == 0.0;
    Some(widen_min(avg, pct, integral))
}

fn range(enabled: bool, min: Option<u32>) -> RangeFilter {
    RangeFilter { enabled: enabled && min.is_some(), min: min.map(f64::from), max: None }
}

/// Build default filters for an item.
///
/// * uniques search by `name` + `type`, stats present but disabled;
/// * rares/magic/normal search by base `type` + trade category, `nonunique` rarity, with
///   the first [`DEFAULT_ENABLED_STATS`] explicit-like stats enabled (all of them with
///   `enable_all_stats`); implicit, rune and enchant stats start disabled;
/// * unresolved magic names (affixes still in `base_type`) drop the `type` filter;
/// * gems enable gem level and quality, waystones enable tier, normal/magic bases item level;
/// * currency (and uncut gems) use the bulk-exchange route.
pub fn build_filters(item: &ParsedItem, opts: &QueryOptions) -> FilterSet {
    let unique = item.rarity == Rarity::Unique && item.category == ItemCategory::Unique;
    let currency_like = matches!(item.category, ItemCategory::Currency | ItemCategory::Gem);
    let route = if item.category == ItemCategory::Currency || item.class == ItemClass::UncutGem {
        QueryRoute::Exchange
    } else {
        QueryRoute::Search
    };
    let unresolved_magic = item.rarity == Rarity::Magic && item.name.is_none() && !item.unidentified;
    let (name, base_type) = if unique {
        (item.name.clone(), Some(item.base_type.clone()))
    } else if unresolved_magic {
        (None, None)
    } else {
        (None, Some(item.base_type.clone()))
    };
    let rarity = if unique {
        Some("unique".to_owned())
    } else if currency_like || item.category == ItemCategory::Other {
        None
    } else {
        Some("nonunique".to_owned())
    };

    let mut explicit_seen = 0usize;
    let stats = item
        .modifiers
        .iter()
        .filter(|m| m.kind != ModKind::Skill)
        .filter_map(|m| {
            let stat_id = m.stat_id.clone()?;
            let explicit_like = matches!(
                m.kind,
                ModKind::Explicit | ModKind::Fractured | ModKind::Desecrated | ModKind::Crafted | ModKind::Sanctified
            );
            let enabled = !unique
                && explicit_like
                && (opts.enable_all_stats || {
                    explicit_seen += 1;
                    explicit_seen <= DEFAULT_ENABLED_STATS
                });
            Some(StatFilter {
                stat_id,
                text: m.text.clone(),
                enabled,
                min: widen_values(&m.values, opts.widen_pct),
                max: None,
                item_values: m.values.clone(),
            })
        })
        .collect();

    let gem = item.category == ItemCategory::Gem;
    let base_item = matches!(item.rarity, Rarity::Normal | Rarity::Magic)
        && matches!(item.category, ItemCategory::Equipment | ItemCategory::Jewel);
    let misc = MiscFilters {
        item_level: range(base_item, item.item_level),
        quality: range(gem, item.quality),
        gem_level: range(gem, item.gem_level),
        waystone_tier: RangeFilter {
            enabled: item.waystone_tier.is_some(),
            min: item.waystone_tier.map(f64::from),
            max: item.waystone_tier.map(f64::from),
        },
        sockets: range(false, (item.sockets > 0).then_some(u32::from(item.sockets))),
        corrupted: if item.corrupted {
            Some(true)
        } else if gem || unique {
            Some(false)
        } else {
            None
        },
        identified: item.unidentified.then_some(false),
    };

    FilterSet {
        league: opts.league.clone(),
        status: if opts.online_only { OnlineStatus::Online } else { OnlineStatus::Any },
        name,
        base_type,
        category: item.class.trade_category().map(str::to_owned),
        rarity,
        stats,
        misc,
        widen_pct: opts.widen_pct,
        route,
    }
}

fn range_json(r: &RangeFilter) -> Option<Value> {
    if !r.enabled || (r.min.is_none() && r.max.is_none()) {
        return None;
    }
    Some(min_max(r.min, r.max))
}

fn min_max(min: Option<f64>, max: Option<f64>) -> Value {
    let mut m = Map::new();
    if let Some(v) = min {
        m.insert("min".into(), json!(v));
    }
    if let Some(v) = max {
        m.insert("max".into(), json!(v));
    }
    Value::Object(m)
}

fn bool_option(v: bool) -> Value {
    json!({ "option": if v { "true" } else { "false" } })
}

/// Insert `{ "<group>": { "filters": { ... } } }` when the group has entries.
fn group(out: &mut Map<String, Value>, name: &str, entries: Vec<(&str, Option<Value>)>) {
    let filters: Map<String, Value> = entries.into_iter().filter_map(|(k, v)| v.map(|v| (k.to_owned(), v))).collect();
    if !filters.is_empty() {
        out.insert(name.to_owned(), json!({ "filters": filters }));
    }
}

impl FilterSet {
    /// Re-apply `widen_pct` to every stat's min/max from `item_values`.
    pub fn rewiden(&mut self, pct: f64) {
        self.widen_pct = pct;
        for s in &mut self.stats {
            if let Some(min) = widen_values(&s.item_values, pct) {
                s.min = Some(min);
                s.max = None;
            }
        }
    }

    /// Serialise to the trade2 search body.
    ///
    /// Shape (verified against pathofexile.com/trade2 requests and `/api/trade2/data/filters`):
    /// ```json
    /// {
    ///   "query": {
    ///     "status": { "option": "securable" | "online" | "any" },
    ///     "name": "Goldrim",               // uniques only
    ///     "type": "Expert Hunter Hood",    // base type
    ///     "stats": [{ "type": "and", "filters": [
    ///       { "id": "explicit.stat_3299347043", "value": { "min": 38 }, "disabled": false }
    ///     ]}],
    ///     "filters": {
    ///       "type_filters":      { "filters": { "category": { "option": "armour.helmet" },
    ///                                           "rarity": { "option": "nonunique" },
    ///                                           "ilvl": { "min": 79 }, "quality": { "min": 20 } } },
    ///       "misc_filters":      { "filters": { "gem_level": { "min": 20 },
    ///                                           "corrupted": { "option": "true" },
    ///                                           "identified": { "option": "false" } } },
    ///       "map_filters":       { "filters": { "map_tier": { "min": 15, "max": 15 } } },
    ///       "equipment_filters": { "filters": { "rune_sockets": { "min": 2 } } }
    ///     }
    ///   },
    ///   "sort": { "price": "asc" }
    /// }
    /// ```
    /// Boolean options are the strings `"true"`/`"false"`; empty filter groups are omitted;
    /// disabled range filters are omitted while disabled stats are sent with `"disabled": true`
    /// so the trade site shows them unticked. The [`QueryRoute::Exchange`] route uses a
    /// different endpoint body (`have`/`want` static ids) built by `kalandra-trade`; this
    /// method always returns the search shape.
    pub fn to_trade_query(&self) -> TradeQuery {
        let status = match self.status {
            OnlineStatus::Securable => "securable",
            OnlineStatus::Online => "online",
            OnlineStatus::Any => "any",
        };
        let mut query = Map::new();
        query.insert("status".into(), json!({ "option": status }));
        if let Some(name) = &self.name {
            query.insert("name".into(), json!(name));
        }
        if let Some(t) = &self.base_type {
            query.insert("type".into(), json!(t));
        }
        let stat_filters: Vec<Value> = self
            .stats
            .iter()
            .map(|s| json!({ "id": s.stat_id, "value": min_max(s.min, s.max), "disabled": !s.enabled }))
            .collect();
        query.insert("stats".into(), json!([{ "type": "and", "filters": stat_filters }]));

        let mut filters = Map::new();
        let m = &self.misc;
        group(
            &mut filters,
            "type_filters",
            vec![
                ("category", self.category.as_ref().map(|c| json!({ "option": c }))),
                ("rarity", self.rarity.as_ref().map(|r| json!({ "option": r }))),
                ("ilvl", range_json(&m.item_level)),
                ("quality", range_json(&m.quality)),
            ],
        );
        group(
            &mut filters,
            "misc_filters",
            vec![
                ("gem_level", range_json(&m.gem_level)),
                ("corrupted", m.corrupted.map(bool_option)),
                ("identified", m.identified.map(bool_option)),
            ],
        );
        group(&mut filters, "map_filters", vec![("map_tier", range_json(&m.waystone_tier))]);
        group(&mut filters, "equipment_filters", vec![("rune_sockets", range_json(&m.sockets))]);
        query.insert("filters".into(), Value::Object(filters));

        TradeQuery { query: Value::Object(query), sort: json!({ "price": "asc" }) }
    }
}

#[cfg(test)]
mod tests {
    use pretty_assertions::assert_eq;
    use proptest::prelude::*;

    use super::*;
    use crate::{matcher::StatMatcher, parser::parse_item};

    fn opts() -> QueryOptions {
        QueryOptions { league: "Standard".into(), online_only: true, widen_pct: 10.0, enable_all_stats: false }
    }

    fn rare_bow() -> ParsedItem {
        let text = "Item Class: Bows\nRarity: Rare\nHate Thirst\nRecurve Bow\n--------\nItem Level: 80\n--------\n+20% to Fire Resistance (implicit)\n--------\n+42 to maximum Life\n+12% to Fire Resistance\n+31% to Cold Resistance\nAdds 3 to 8 Physical Damage\n10% reduced Movement Speed\n--------\nCorrupted";
        let mut item = parse_item(text).unwrap();
        let cat = crate::matcher::tests::fake_catalog();
        StatMatcher::new(&cat).match_item(&mut item);
        item
    }

    #[test]
    fn rare_defaults() {
        let f = build_filters(&rare_bow(), &opts());
        assert_eq!(f.route, QueryRoute::Search);
        assert_eq!(f.status, OnlineStatus::Online);
        assert_eq!(f.base_type.as_deref(), Some("Recurve Bow"));
        assert_eq!(f.name, None);
        assert_eq!(f.category.as_deref(), Some("weapon.bow"));
        assert_eq!(f.rarity.as_deref(), Some("nonunique"));
        assert_eq!(f.stats.len(), 6);
        assert!(!f.stats[0].enabled, "implicit disabled");
        assert_eq!(f.stats.iter().filter(|s| s.enabled).count(), DEFAULT_ENABLED_STATS);
        assert_eq!(f.stats[1].min, Some(37.0));
        assert_eq!(f.stats[4].min, Some(4.95)); // avg of 3 and 8, fractional
        assert_eq!(f.misc.corrupted, Some(true));
        assert!(!f.misc.item_level.enabled);

        let all = build_filters(&rare_bow(), &QueryOptions { enable_all_stats: true, online_only: false, ..opts() });
        assert_eq!(all.stats.iter().filter(|s| s.enabled).count(), 5);
        assert_eq!(all.status, OnlineStatus::Any);
    }

    #[test]
    fn rewiden_recomputes() {
        let mut f = build_filters(&rare_bow(), &opts());
        f.rewiden(0.0);
        assert!(f.widen_pct.abs() < f64::EPSILON);
        assert_eq!(f.stats[1].min, Some(42.0));
        f.rewiden(50.0);
        assert_eq!(f.stats[1].min, Some(21.0));
        // Negated stat (reduced movement speed -> -10) widens downwards.
        assert_eq!(f.stats[5].item_values, vec![-10.0]);
        assert_eq!(f.stats[5].min, Some(-15.0));
    }

    #[test]
    fn trade_query_shape() {
        let mut f = build_filters(&rare_bow(), &opts());
        f.status = OnlineStatus::Securable;
        f.misc.sockets = RangeFilter { enabled: true, min: Some(2.0), max: None };
        f.misc.identified = Some(false);
        let q = serde_json::to_value(f.to_trade_query()).unwrap();
        assert_eq!(q["query"]["status"]["option"], "securable");
        assert_eq!(q["query"]["type"], "Recurve Bow");
        assert_eq!(q["query"]["stats"][0]["type"], "and");
        assert_eq!(q["query"]["stats"][0]["filters"][0]["disabled"], true);
        assert_eq!(q["query"]["stats"][0]["filters"][1]["value"]["min"], 37.0);
        assert_eq!(q["query"]["filters"]["type_filters"]["filters"]["category"]["option"], "weapon.bow");
        assert_eq!(q["query"]["filters"]["misc_filters"]["filters"]["corrupted"]["option"], "true");
        assert_eq!(q["query"]["filters"]["misc_filters"]["filters"]["identified"]["option"], "false");
        assert_eq!(q["query"]["filters"]["equipment_filters"]["filters"]["rune_sockets"]["min"], 2.0);
        assert!(q["query"]["filters"].get("map_filters").is_none());
        assert_eq!(q["sort"]["price"], "asc");
    }

    #[test]
    fn unique_gem_waystone_currency() {
        let unique = parse_item("Item Class: Helmets\nRarity: Unique\nGoldrim\nFelt Cap\n--------\nItem Level: 50\n--------\n+42 to maximum Life").unwrap();
        let mut unique = unique;
        let cat = crate::matcher::tests::fake_catalog();
        StatMatcher::new(&cat).match_item(&mut unique);
        let f = build_filters(&unique, &opts());
        assert_eq!(f.name.as_deref(), Some("Goldrim"));
        assert_eq!(f.base_type.as_deref(), Some("Felt Cap"));
        assert_eq!(f.rarity.as_deref(), Some("unique"));
        assert!(f.stats.iter().all(|s| !s.enabled));
        assert_eq!(f.misc.corrupted, Some(false));
        let q = f.to_trade_query();
        assert_eq!(q.query["name"], "Goldrim");

        let gem = parse_item("Item Class: Skill Gems\nRarity: Gem\nFireball\n--------\nLevel: 20 (Max)\nQuality: +20%")
            .unwrap();
        let f = build_filters(&gem, &opts());
        assert!(f.misc.gem_level.enabled && f.misc.quality.enabled);
        assert_eq!(f.rarity, None);
        let q = serde_json::to_value(f.to_trade_query()).unwrap();
        assert_eq!(q["query"]["filters"]["misc_filters"]["filters"]["gem_level"]["min"], 20.0);
        assert_eq!(q["query"]["filters"]["type_filters"]["filters"]["quality"]["min"], 20.0);

        let way = parse_item("Item Class: Waystones\nRarity: Normal\nWaystone (Tier 15)").unwrap();
        let q = serde_json::to_value(build_filters(&way, &opts()).to_trade_query()).unwrap();
        assert_eq!(q["query"]["filters"]["map_filters"]["filters"]["map_tier"], json!({"min": 15.0, "max": 15.0}));

        let orb = parse_item("Item Class: Stackable Currency\nRarity: Currency\nDivine Orb").unwrap();
        assert_eq!(build_filters(&orb, &opts()).route, QueryRoute::Exchange);

        let magic =
            parse_item("Item Class: Rings\nRarity: Magic\nHale Ruby Ring of the Kiln\n--------\nItem Level: 40")
                .unwrap();
        let f = build_filters(&magic, &opts());
        assert_eq!(f.base_type, None);
        assert!(f.misc.item_level.enabled);
    }

    #[test]
    fn range_json_edges() {
        assert_eq!(range_json(&RangeFilter { enabled: true, min: None, max: None }), None);
        assert_eq!(range_json(&RangeFilter { enabled: false, min: Some(1.0), max: None }), None);
        assert_eq!(range_json(&RangeFilter { enabled: true, min: None, max: Some(3.0) }), Some(json!({"max": 3.0})));
        assert_eq!(widen_values(&[], 10.0), None);
    }

    proptest! {
        #[test]
        fn widen_never_exceeds_value(v in -10_000.0f64..10_000.0, pct in -50.0f64..200.0, integral: bool) {
            let v = if integral { v.round() } else { v };
            let min = widen_min(v, pct, integral);
            prop_assert!(min <= v);
            // Never widened beyond 100% (+ rounding slack).
            prop_assert!(min >= v - v.abs() - 1.0);
        }
    }
}
