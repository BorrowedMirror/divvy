//! kalandra-trade: network side. Official trade2 API, poe.ninja and poe2scout behind
//! [`PriceSource`], with a header-driven [`ratelimit::RateLimiter`] and a moka cache.
pub mod client;
pub mod error;
pub mod ninja;
pub mod ratelimit;
pub mod scout;
pub mod source;
pub mod types;

pub use client::{TradeClient, TradeConfig};
pub use error::TradeError;
pub use source::{PriceRequest, PriceSource};

/// Default User-Agent. GGG asks for a descriptive UA with contact details; users can
/// append their own contact e-mail via settings (`trade.contact`).
pub fn default_user_agent() -> String {
    format!(
        "kalandra/{} (+https://github.com/nightah/kalandra; contact: https://github.com/nightah/kalandra/issues)",
        env!("CARGO_PKG_VERSION")
    )
}
