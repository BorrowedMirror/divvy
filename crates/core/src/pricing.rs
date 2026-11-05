//! Price aggregation over listings.
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct Price {
    pub amount: f64,
    /// Trade currency id, e.g. `divine`, `exalted`, `chaos`.
    pub currency: String,
}

/// One listing from a price source.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct Listing {
    pub id: String,
    pub price: Price,
    pub seller: Option<String>,
    pub online: bool,
    /// RFC 3339 listing timestamp.
    pub indexed: Option<String>,
    pub item_level: Option<u32>,
    pub stack_size: Option<u32>,
}

/// Where a price came from.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum PriceSourceKind {
    Trade,
    PoeNinja,
    Poe2Scout,
}

/// Aggregated result displayed by the overlay.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct PriceSummary {
    pub source: PriceSourceKind,
    /// Total matches reported by the source (may exceed `listings.len()`).
    pub total: u32,
    pub listings: Vec<Listing>,
    /// Min / median / max normalised to the most common currency among listings.
    pub min: Option<Price>,
    pub median: Option<Price>,
    pub max: Option<Price>,
    /// Link that opens the same search on pathofexile.com.
    pub trade_url: Option<String>,
}

/// Exchange rates to normalise prices into one currency; `rates[c]` = value of 1 `c` in `base`.
#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize, specta::Type)]
pub struct ExchangeRates {
    pub base: String,
    pub rates: std::collections::BTreeMap<String, f64>,
}

/// Aggregate listings into a summary. Pure; outlier trimming lives here.
pub fn summarise(
    _source: PriceSourceKind,
    _total: u32,
    _listings: Vec<Listing>,
    _rates: Option<&ExchangeRates>,
) -> PriceSummary {
    todo!("workstream A")
}
