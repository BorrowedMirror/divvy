//! poe2scout aggregated prices (`https://api.poe2scout.com`, see docs/research/ninja-scout.md).
use std::{sync::Arc, time::Duration};

use async_trait::async_trait;
use kalandra_core::{
    item::{ItemCategory, ParsedItem},
    pricing::{ExchangeRates, PriceSourceKind, PriceSummary},
};
use moka::future::Cache;
use serde::Deserialize;
use url::Url;

use crate::{
    PriceRequest, PriceSource, TradeError,
    rates::{aggregated_summary, display_price, exchange_name},
};

const SOURCE: &str = "poe2scout";
const REALM: &str = "poe2";
/// poe2scout refreshes far less often than this.
const DEFAULT_TTL: Duration = Duration::from_secs(600);

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "PascalCase")]
struct ScoutLeague {
    value: String,
    #[serde(default)]
    divine_price: Option<f64>,
    #[serde(default)]
    chaos_divine_price: Option<f64>,
    #[serde(default)]
    base_currency_api_id: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "PascalCase")]
struct ScoutItem {
    text: String,
    #[serde(default)]
    name: Option<String>,
    #[serde(default, rename = "Type")]
    kind: Option<String>,
    #[serde(default)]
    current_price: Option<f64>,
}

/// Aggregated-price source backed by https://poe2scout.com (currency, uniques, gems).
#[derive(Debug, Clone)]
pub struct Poe2ScoutSource {
    pub base_url: Url,
    pub user_agent: String,
    http: reqwest::Client,
    leagues: Cache<(), Arc<Vec<ScoutLeague>>>,
    items: Cache<String, Arc<Vec<ScoutItem>>>,
}

impl Poe2ScoutSource {
    pub fn new(base_url: Url, user_agent: String) -> Self {
        Self::with_cache_ttl(base_url, user_agent, DEFAULT_TTL)
    }

    /// Like [`Self::new`] with a custom cache lifetime for the league/item lists.
    pub fn with_cache_ttl(base_url: Url, user_agent: String, ttl: Duration) -> Self {
        let http = reqwest::Client::builder().user_agent(user_agent.clone()).build().unwrap_or_default();
        Self {
            base_url,
            user_agent,
            http,
            leagues: Cache::builder().max_capacity(1).time_to_live(ttl).build(),
            items: Cache::builder().max_capacity(16).time_to_live(ttl).build(),
        }
    }

    fn url(&self, segments: &[&str]) -> Url {
        let mut url = self.base_url.clone();
        if let Ok(mut path) = url.path_segments_mut() {
            path.pop_if_empty().extend(segments);
        }
        url
    }

    async fn get_json<T: serde::de::DeserializeOwned>(&self, url: Url) -> Result<T, TradeError> {
        let resp = self.http.get(url).send().await?;
        let status = resp.status();
        let body = resp.bytes().await?;
        if !status.is_success() {
            let message = String::from_utf8_lossy(&body).chars().take(200).collect();
            return Err(TradeError::Http { status: status.as_u16(), message });
        }
        serde_json::from_slice(&body).map_err(|e| TradeError::Decode(format!("{SOURCE}: {e}")))
    }

    async fn leagues(&self) -> Result<Arc<Vec<ScoutLeague>>, TradeError> {
        if let Some(v) = self.leagues.get(&()).await {
            return Ok(v);
        }
        let v: Arc<Vec<ScoutLeague>> = Arc::new(self.get_json(self.url(&[REALM, "Leagues"])).await?);
        self.leagues.insert((), v.clone()).await;
        Ok(v)
    }

    async fn items(&self, league: &str) -> Result<Arc<Vec<ScoutItem>>, TradeError> {
        if let Some(v) = self.items.get(league).await {
            return Ok(v);
        }
        let v: Arc<Vec<ScoutItem>> = Arc::new(self.get_json(self.url(&[REALM, "Leagues", league, "Items"])).await?);
        self.items.insert(league.to_owned(), v.clone()).await;
        Ok(v)
    }

    /// Exchange rates for `league`, base = the league's base currency (exalted).
    pub async fn rates(&self, league: &str) -> Result<ExchangeRates, TradeError> {
        let leagues = self.leagues().await?;
        let l = leagues.iter().find(|l| l.value == league).ok_or(TradeError::Unsupported(SOURCE))?;
        let mut rates = ExchangeRates {
            base: l.base_currency_api_id.clone().unwrap_or_else(|| "exalted".into()),
            rates: std::collections::BTreeMap::new(),
        };
        if let Some(div) = l.divine_price.filter(|d| *d > 0.0) {
            rates.rates.insert("divine".into(), div);
            if let Some(cd) = l.chaos_divine_price.filter(|c| *c > 0.0) {
                rates.rates.insert("chaos".into(), div / cd);
            }
        }
        Ok(rates)
    }
}

fn find<'a>(items: &'a [ScoutItem], item: &ParsedItem) -> Option<&'a ScoutItem> {
    if item.category == ItemCategory::Unique {
        let name = item.name.as_deref()?;
        let by_name = |i: &&ScoutItem| i.name.as_deref() == Some(name);
        items
            .iter()
            .filter(by_name)
            .find(|i| i.kind.as_deref() == Some(item.base_type.as_str()))
            .or_else(|| items.iter().find(by_name))
    } else {
        let text = exchange_name(item);
        items.iter().find(|i| i.text == text)
    }
}

#[async_trait]
impl PriceSource for Poe2ScoutSource {
    fn kind(&self) -> PriceSourceKind {
        PriceSourceKind::Poe2Scout
    }

    fn supports(&self, item: &ParsedItem) -> bool {
        match item.category {
            ItemCategory::Currency | ItemCategory::Gem => true,
            ItemCategory::Unique => item.name.is_some(),
            _ => false,
        }
    }

    async fn price(&self, req: &PriceRequest) -> Result<PriceSummary, TradeError> {
        if !self.supports(&req.item) {
            return Err(TradeError::Unsupported(SOURCE));
        }
        let league = req.filters.league.as_str();
        let rates = self.rates(league).await?;
        let items = self.items(league).await?;
        let value = find(&items, &req.item).and_then(|i| i.current_price).ok_or(TradeError::Unsupported(SOURCE))?;
        Ok(aggregated_summary(PriceSourceKind::Poe2Scout, display_price(value, &rates), 1))
    }
}
