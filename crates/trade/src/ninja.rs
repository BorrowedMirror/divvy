use async_trait::async_trait;
use kalandra_core::{
    item::ParsedItem,
    pricing::{PriceSourceKind, PriceSummary},
};
use url::Url;

use crate::{PriceRequest, PriceSource, TradeError};

/// Aggregated-price source backed by https://poe.ninja (currency, uniques, gems).
#[derive(Debug, Clone)]
pub struct PoeNinjaSource {
    pub base_url: Url,
    pub user_agent: String,
}

impl PoeNinjaSource {
    pub fn new(base_url: Url, user_agent: String) -> Self {
        Self { base_url, user_agent }
    }
}

#[async_trait]
impl PriceSource for PoeNinjaSource {
    fn kind(&self) -> PriceSourceKind {
        PriceSourceKind::PoeNinja
    }
    fn supports(&self, _item: &ParsedItem) -> bool {
        todo!("workstream B")
    }
    async fn price(&self, _req: &PriceRequest) -> Result<PriceSummary, TradeError> {
        todo!("workstream B")
    }
}
