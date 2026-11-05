//! poe.ninja PoE2 economy API (see docs/research/ninja-scout.md).
//!
//! Currency-like items come from `/poe2/api/economy/exchange/current/overview`,
//! uniques from `/poe2/api/economy/stash/current/item/overview`. Values are in
//! `core.primary` (divine) and `core.rates[c]` is how many `c` one primary buys.
use std::{collections::BTreeMap, sync::Arc, time::Duration};

use async_trait::async_trait;
use moka::future::Cache;
use rkl_core::{
    item::{ItemCategory, ItemClass, ParsedItem},
    pricing::{ExchangeRates, PriceSourceKind, PriceSummary},
};
use serde::Deserialize;
use url::Url;

use crate::{PriceRequest, PriceSource, TradeError, rates};

/// poe.ninja refreshes PoE2 overviews roughly hourly; ten minutes keeps us well
/// inside their "don't hammer the economy endpoints" policy.
const DEFAULT_TTL: Duration = Duration::from_secs(600);
const SOURCE: &str = "poe.ninja";

const EXCHANGE_PATH: &str = "poe2/api/economy/exchange/current/overview";
const STASH_PATH: &str = "poe2/api/economy/stash/current/item/overview";

/// Every exchange overview type, in lookup order for unknown currency-like classes.
const ALL_EXCHANGE: &[&str] = &[
    "Currency",
    "Fragments",
    "Runes",
    "Essences",
    "SoulCores",
    "Ritual",
    "Breach",
    "Delirium",
    "Expedition",
    "Abyss",
    "Verisium",
    "Idols",
    "UncutGems",
    "LineageSupportGems",
];

const ALL_STASH: &[&str] = &[
    "UniqueAccessories",
    "UniqueArmours",
    "UniqueWeapons",
    "UniqueFlasks",
    "UniqueCharms",
    "UniqueJewels",
    "UniqueSanctumRelics",
    "UniqueTablets",
    "PrecursorTablets",
];

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Endpoint {
    Exchange,
    Stash,
}

impl Endpoint {
    fn path(self) -> &'static str {
        match self {
            Self::Exchange => EXCHANGE_PATH,
            Self::Stash => STASH_PATH,
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
struct Core {
    primary: String,
    #[serde(default)]
    rates: BTreeMap<String, f64>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct Line {
    /// String trade id on exchange overviews, numeric on stash overviews.
    id: serde_json::Value,
    primary_value: f64,
    #[serde(default)]
    name: Option<String>,
    #[serde(default)]
    base_type: Option<String>,
    #[serde(default)]
    listing_count: Option<u32>,
}

#[derive(Debug, Clone, Deserialize)]
struct ExchangeItem {
    id: String,
    name: String,
}

#[derive(Debug, Clone, Deserialize)]
struct Overview {
    core: Core,
    #[serde(default)]
    lines: Vec<Line>,
    #[serde(default)]
    items: Vec<ExchangeItem>,
}

impl Overview {
    fn rates(&self) -> ExchangeRates {
        ExchangeRates {
            base: self.core.primary.clone(),
            rates: self
                .core
                .rates
                .iter()
                .filter(|(_, v)| v.is_finite() && **v > 0.0)
                .map(|(k, v)| (k.clone(), 1.0 / v))
                .collect(),
        }
    }

    /// Value (in `core.primary`) and listing count for `item`, if listed.
    fn find(&self, endpoint: Endpoint, item: &ParsedItem) -> Option<(f64, u32)> {
        match endpoint {
            Endpoint::Exchange => {
                let name = rates::exchange_name(item);
                let id = &self.items.iter().find(|i| i.name.eq_ignore_ascii_case(&name))?.id;
                let line = self.lines.iter().find(|l| l.id.as_str() == Some(id))?;
                Some((line.primary_value, 1))
            }
            Endpoint::Stash => {
                let name = item.name.as_deref()?;
                let by_name = |l: &&Line| l.name.as_deref() == Some(name);
                let line = self
                    .lines
                    .iter()
                    .filter(by_name)
                    .find(|l| l.base_type.as_deref() == Some(item.base_type.as_str()))
                    .or_else(|| self.lines.iter().find(by_name))?;
                Some((line.primary_value, line.listing_count.unwrap_or(1)))
            }
        }
    }
}

/// Exchange overview types to try for a currency-like item class.
fn exchange_types(class: &ItemClass) -> &'static [&'static str] {
    match class {
        ItemClass::StackableCurrency => {
            &["Currency", "Fragments", "Expedition", "Verisium", "Abyss", "Ritual", "Breach", "Delirium", "Idols"]
        }
        ItemClass::Omen => &["Ritual"],
        ItemClass::Essence => &["Essences"],
        ItemClass::SoulCore => &["SoulCores"],
        ItemClass::Rune => &["Runes"],
        ItemClass::Talisman => &["Runes", "Idols"],
        ItemClass::DistilledEmotion => &["Delirium"],
        ItemClass::Splinter => &["Breach", "Fragments", "Delirium", "Ritual"],
        ItemClass::Catalyst => &["Breach"],
        ItemClass::UncutGem => &["UncutGems"],
        ItemClass::SupportGem => &["LineageSupportGems"],
        _ => ALL_EXCHANGE,
    }
}

/// Stash overview types to try for a unique of this class.
fn stash_types(class: &ItemClass) -> &'static [&'static str] {
    match class {
        ItemClass::Amulet | ItemClass::Ring | ItemClass::Belt => &["UniqueAccessories"],
        ItemClass::BodyArmour
        | ItemClass::Helmet
        | ItemClass::Gloves
        | ItemClass::Boots
        | ItemClass::Shield
        | ItemClass::Buckler
        | ItemClass::Focus => &["UniqueArmours"],
        ItemClass::Quiver => &["UniqueArmours", "UniqueWeapons"],
        ItemClass::Weapon(_) => &["UniqueWeapons"],
        ItemClass::LifeFlask | ItemClass::ManaFlask => &["UniqueFlasks"],
        ItemClass::Charm => &["UniqueCharms"],
        ItemClass::Jewel => &["UniqueJewels"],
        ItemClass::Relic => &["UniqueSanctumRelics"],
        ItemClass::Tablet => &["UniqueTablets", "PrecursorTablets"],
        _ => ALL_STASH,
    }
}

fn candidates(item: &ParsedItem) -> Vec<(Endpoint, &'static str)> {
    match item.category {
        ItemCategory::Unique => stash_types(&item.class).iter().map(|t| (Endpoint::Stash, *t)).collect(),
        _ => exchange_types(&item.class).iter().map(|t| (Endpoint::Exchange, *t)).collect(),
    }
}

/// Aggregated-price source backed by https://poe.ninja (currency, uniques, gems).
#[derive(Debug, Clone)]
pub struct PoeNinjaSource {
    pub base_url: Url,
    pub user_agent: String,
    http: reqwest::Client,
    cache: Cache<(String, String), Arc<Overview>>,
}

fn new_cache(ttl: Duration) -> Cache<(String, String), Arc<Overview>> {
    Cache::builder().max_capacity(64).time_to_live(ttl).build()
}

impl PoeNinjaSource {
    pub fn new(base_url: Url, user_agent: String) -> Self {
        let http = reqwest::Client::builder()
            .user_agent(user_agent.clone())
            .build()
            .unwrap_or_else(|_| reqwest::Client::new());
        Self { base_url, user_agent, http, cache: new_cache(DEFAULT_TTL) }
    }

    /// Replace the response cache with one using `ttl` (default 10 minutes).
    #[must_use]
    pub fn with_cache_ttl(mut self, ttl: Duration) -> Self {
        self.cache = new_cache(ttl);
        self
    }

    fn url(&self, endpoint: Endpoint, league: &str, kind: &str) -> Result<Url, TradeError> {
        let mut url = self.base_url.join(endpoint.path()).map_err(|e| TradeError::Config(e.to_string()))?;
        url.query_pairs_mut().append_pair("league", league).append_pair("type", kind);
        Ok(url)
    }

    async fn overview(&self, endpoint: Endpoint, league: &str, kind: &str) -> Result<Arc<Overview>, TradeError> {
        let key = (league.to_owned(), format!("{}/{kind}", endpoint.path()));
        if let Some(hit) = self.cache.get(&key).await {
            return Ok(hit);
        }
        let url = self.url(endpoint, league, kind)?;
        let resp = self.http.get(url).send().await?;
        let status = resp.status();
        let body = resp.bytes().await?;
        if !status.is_success() {
            let text = String::from_utf8_lossy(&body);
            return Err(TradeError::Http { status: status.as_u16(), message: text.chars().take(200).collect() });
        }
        let parsed: Overview =
            serde_json::from_slice(&body).map_err(|e| TradeError::Decode(format!("{SOURCE} {kind}: {e}")))?;
        let parsed = Arc::new(parsed);
        self.cache.insert(key, parsed.clone()).await;
        Ok(parsed)
    }

    /// Current exchange rates for `league` (base = divine), from the Currency overview.
    pub async fn rates(&self, league: &str) -> Result<ExchangeRates, TradeError> {
        Ok(self.overview(Endpoint::Exchange, league, "Currency").await?.rates())
    }
}

#[async_trait]
impl PriceSource for PoeNinjaSource {
    fn kind(&self) -> PriceSourceKind {
        PriceSourceKind::PoeNinja
    }

    fn supports(&self, item: &ParsedItem) -> bool {
        match item.category {
            ItemCategory::Currency => true,
            ItemCategory::Unique => item.name.is_some(),
            ItemCategory::Gem => matches!(item.class, ItemClass::UncutGem | ItemClass::SupportGem),
            _ => false,
        }
    }

    async fn price(&self, req: &PriceRequest) -> Result<PriceSummary, TradeError> {
        let item = &req.item;
        if !self.supports(item) {
            return Err(TradeError::Unsupported(SOURCE));
        }
        let league = &req.filters.league;
        for (endpoint, kind) in candidates(item) {
            let overview = self.overview(endpoint, league, kind).await?;
            if let Some((value, total)) = overview.find(endpoint, item) {
                let price = rates::display_price(value, &overview.rates());
                return Ok(rates::aggregated_summary(PriceSourceKind::PoeNinja, price, total));
            }
        }
        Err(TradeError::Unsupported(SOURCE))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::rates::test_support::item;

    #[test]
    fn candidate_mapping() {
        let unique = |class| candidates(&item(class, ItemCategory::Unique, Some("X"), "Y"));
        assert_eq!(unique(ItemClass::Ring), vec![(Endpoint::Stash, "UniqueAccessories")]);
        assert_eq!(unique(ItemClass::Helmet), vec![(Endpoint::Stash, "UniqueArmours")]);
        assert_eq!(unique(ItemClass::Quiver).len(), 2);
        assert_eq!(unique(ItemClass::Weapon("Bows".into())), vec![(Endpoint::Stash, "UniqueWeapons")]);
        assert_eq!(unique(ItemClass::LifeFlask), vec![(Endpoint::Stash, "UniqueFlasks")]);
        assert_eq!(unique(ItemClass::Charm), vec![(Endpoint::Stash, "UniqueCharms")]);
        assert_eq!(unique(ItemClass::Jewel), vec![(Endpoint::Stash, "UniqueJewels")]);
        assert_eq!(unique(ItemClass::Relic), vec![(Endpoint::Stash, "UniqueSanctumRelics")]);
        assert_eq!(unique(ItemClass::Tablet).len(), 2);
        assert_eq!(unique(ItemClass::Other("?".into())).len(), ALL_STASH.len());

        let cur = |class| candidates(&item(class, ItemCategory::Currency, None, "Y"));
        assert_eq!(cur(ItemClass::StackableCurrency)[0], (Endpoint::Exchange, "Currency"));
        assert_eq!(cur(ItemClass::Omen), vec![(Endpoint::Exchange, "Ritual")]);
        assert_eq!(cur(ItemClass::Essence), vec![(Endpoint::Exchange, "Essences")]);
        assert_eq!(cur(ItemClass::SoulCore), vec![(Endpoint::Exchange, "SoulCores")]);
        assert_eq!(cur(ItemClass::Rune), vec![(Endpoint::Exchange, "Runes")]);
        assert_eq!(cur(ItemClass::Talisman).len(), 2);
        assert_eq!(cur(ItemClass::DistilledEmotion), vec![(Endpoint::Exchange, "Delirium")]);
        assert_eq!(cur(ItemClass::Splinter).len(), 4);
        assert_eq!(cur(ItemClass::Catalyst), vec![(Endpoint::Exchange, "Breach")]);
        assert_eq!(cur(ItemClass::UncutGem), vec![(Endpoint::Exchange, "UncutGems")]);
        assert_eq!(cur(ItemClass::SupportGem), vec![(Endpoint::Exchange, "LineageSupportGems")]);
        assert_eq!(cur(ItemClass::Other("?".into())).len(), ALL_EXCHANGE.len());
    }

    #[test]
    fn stash_match_falls_back_to_name_only() {
        let ov: Overview = serde_json::from_str(
            r#"{"core":{"primary":"divine","rates":{"exalted":0.0}},
                "lines":[{"id":1,"name":"Foo","baseType":"Other Base","primaryValue":2.0}]}"#,
        )
        .unwrap();
        let it = item(ItemClass::Ring, ItemCategory::Unique, Some("Foo"), "Gold Ring");
        assert_eq!(ov.find(Endpoint::Stash, &it), Some((2.0, 1)));
        // Non-positive rates are dropped.
        assert!(ov.rates().rates.is_empty());
        let nameless = item(ItemClass::Ring, ItemCategory::Unique, None, "Gold Ring");
        assert_eq!(ov.find(Endpoint::Stash, &nameless), None);
    }

    #[test]
    fn bad_base_url_is_config_error() {
        let src = PoeNinjaSource::new(Url::parse("data:text/plain,x").unwrap(), "ua".into());
        assert!(matches!(src.url(Endpoint::Exchange, "L", "Currency"), Err(TradeError::Config(_))));
    }
}
