use async_trait::async_trait;
use kalandra_core::{
    item::ParsedItem,
    pricing::{PriceSourceKind, PriceSummary},
    query::FilterSet,
};

use crate::TradeError;

/// Input to a price lookup.
#[derive(Debug, Clone)]
pub struct PriceRequest {
    pub item: ParsedItem,
    pub filters: FilterSet,
}

/// Anything that can price an item.
#[async_trait]
pub trait PriceSource: Send + Sync + std::fmt::Debug {
    fn kind(&self) -> PriceSourceKind;
    /// Whether this source can price the item at all (e.g. ninja: currency/uniques only).
    fn supports(&self, item: &ParsedItem) -> bool;
    async fn price(&self, req: &PriceRequest) -> Result<PriceSummary, TradeError>;
}
