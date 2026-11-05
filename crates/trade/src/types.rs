//! Wire types for `/api/trade2/*`. Field names match the live JSON (see docs/research/trade2.md).
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct League {
    pub id: String,
    pub realm: Option<String>,
    pub text: Option<String>,
}

/// `GET /api/trade2/data/leagues` -> `{ "result": [...] }`
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResultList<T> {
    pub result: Vec<T>,
}

/// `POST /api/trade2/search/{league}` response.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SearchResponse {
    pub id: String,
    #[serde(default)]
    pub complexity: Option<u32>,
    pub result: Vec<String>,
    pub total: u32,
}

/// `GET /api/trade2/fetch/{ids}?query={id}` response entry. Only fields we use are typed.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FetchEntry {
    pub id: String,
    pub listing: FetchListing,
    #[serde(default)]
    pub item: serde_json::Value,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FetchListing {
    pub indexed: Option<String>,
    pub price: Option<FetchPrice>,
    pub account: Option<FetchAccount>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FetchPrice {
    #[serde(rename = "type")]
    pub kind: Option<String>,
    pub amount: f64,
    pub currency: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FetchAccount {
    pub name: String,
    pub online: Option<serde_json::Value>,
}
