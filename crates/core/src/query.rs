//! Editable filters and trade2 search query construction.
use serde::{Deserialize, Serialize};

use crate::item::ParsedItem;

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

/// Build default filters for an item.
pub fn build_filters(_item: &ParsedItem, _opts: &QueryOptions) -> FilterSet {
    todo!("workstream A")
}

impl FilterSet {
    /// Re-apply `widen_pct` to every stat's min/max from `item_values`.
    pub fn rewiden(&mut self, _pct: f64) {
        todo!("workstream A")
    }

    /// Serialise to the trade2 search body.
    pub fn to_trade_query(&self) -> TradeQuery {
        todo!("workstream A")
    }
}
