//! rkl-core: pure domain logic. No I/O, no platform, no network.
//!
//! Pipeline: clipboard text --[`parser::parse_item`]--> [`item::ParsedItem`]
//! --[`matcher::StatMatcher`]--> matched stat ids
//! --[`query::build_filters`]--> [`query::FilterSet`] (user-editable)
//! --[`query::FilterSet::to_trade_query`]--> [`query::TradeQuery`] (trade2 JSON body)
//! and listings --[`pricing::summarise`]--> [`pricing::PriceSummary`].

pub mod catalog;
pub mod error;
pub mod item;
pub mod matcher;
pub mod parser;
pub mod pricing;
pub mod pseudo;
pub mod query;

pub use error::{CoreError, ParseError};
pub use item::ParsedItem;
