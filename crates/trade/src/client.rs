use async_trait::async_trait;
use kalandra_core::{
    item::ParsedItem,
    pricing::{PriceSourceKind, PriceSummary},
    query::TradeQuery,
};
use url::Url;

use crate::{
    PriceRequest, PriceSource, TradeError,
    types::{FetchEntry, League, SearchResponse},
};

#[derive(Debug, Clone)]
pub struct TradeConfig {
    /// `https://www.pathofexile.com` in production; wiremock URI in tests.
    pub base_url: Url,
    pub user_agent: String,
    /// Optional POESESSID cookie value.
    pub poesessid: Option<String>,
    /// Max listings to fetch per search (fetch takes <= 10 ids per call).
    pub fetch_count: usize,
    pub cache_ttl: std::time::Duration,
}

impl Default for TradeConfig {
    fn default() -> Self {
        Self {
            base_url: Url::parse("https://www.pathofexile.com").expect("static url"),
            user_agent: crate::default_user_agent(),
            poesessid: None,
            fetch_count: 20,
            cache_ttl: std::time::Duration::from_secs(300),
        }
    }
}

/// Official trade2 API client.
#[derive(Debug)]
pub struct TradeClient {
    config: TradeConfig,
}

impl TradeClient {
    pub fn new(config: TradeConfig, _clock: Box<dyn crate::ratelimit::Clock>) -> Result<Self, TradeError> {
        Ok(Self { config })
    }

    pub fn config(&self) -> &TradeConfig {
        &self.config
    }

    /// `GET /api/trade2/data/leagues`
    pub async fn leagues(&self) -> Result<Vec<League>, TradeError> {
        todo!("workstream B")
    }

    /// `GET /api/trade2/data/{stats|items|static|filters}` raw JSON (used by xtask data refresh).
    pub async fn data(&self, _which: &str) -> Result<serde_json::Value, TradeError> {
        todo!("workstream B")
    }

    /// `POST /api/trade2/search/{league}`
    pub async fn search(&self, _league: &str, _query: &TradeQuery) -> Result<SearchResponse, TradeError> {
        todo!("workstream B")
    }

    /// `GET /api/trade2/fetch/{id,id,...}?query={search_id}`
    pub async fn fetch(&self, _ids: &[String], _search_id: &str) -> Result<Vec<FetchEntry>, TradeError> {
        todo!("workstream B")
    }

    /// `https://www.pathofexile.com/trade2/search/poe2/{league}/{search_id}`
    pub fn trade_url(&self, _league: &str, _search_id: &str) -> Url {
        todo!("workstream B")
    }
}

#[async_trait]
impl PriceSource for TradeClient {
    fn kind(&self) -> PriceSourceKind {
        PriceSourceKind::Trade
    }
    fn supports(&self, _item: &ParsedItem) -> bool {
        true
    }
    async fn price(&self, _req: &PriceRequest) -> Result<PriceSummary, TradeError> {
        todo!("workstream B")
    }
}
