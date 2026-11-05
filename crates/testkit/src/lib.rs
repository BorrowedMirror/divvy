//! kalandra-testkit: shared test support.
//!
//! - [`fakes`]: deterministic in-memory implementations of every `kalandra_platform` trait.
//! - [`corpus`]: the item-text corpus (`crates/testkit/corpus/`).
//! - [`fixtures`]: wiremock helpers mounting recorded trade API responses.
pub mod corpus;
pub mod fakes;
pub mod fixtures;
