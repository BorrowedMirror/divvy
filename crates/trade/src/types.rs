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

/// `POST /api/trade2/search/poe2/{league}` response.
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
    /// Ready-made whisper ("@Seller Hi, I would like to buy ...").
    #[serde(default)]
    pub whisper: Option<String>,
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

/// `POST /api/trade2/exchange/poe2/{league}` response. `result` is an object keyed by
/// listing id (server order = sort order), or `[]` when empty; both decode to a `Vec`.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct ExchangeResponse {
    pub id: String,
    #[serde(default)]
    pub complexity: Option<u32>,
    #[serde(deserialize_with = "ordered_entries")]
    pub result: Vec<ExchangeEntry>,
    pub total: u32,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ExchangeEntry {
    pub id: String,
    pub listing: ExchangeListing,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ExchangeListing {
    pub indexed: Option<String>,
    pub account: Option<FetchAccount>,
    #[serde(default)]
    pub offers: Vec<ExchangeOffer>,
}

/// One offer: the seller gives `item.amount` of `item.currency` for `exchange.amount`
/// of `exchange.currency`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ExchangeOffer {
    pub exchange: ExchangeSide,
    pub item: ExchangeSide,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ExchangeSide {
    pub currency: String,
    pub amount: f64,
    #[serde(default)]
    pub stock: Option<u32>,
}

fn ordered_entries<'de, D: serde::Deserializer<'de>>(d: D) -> Result<Vec<ExchangeEntry>, D::Error> {
    struct V;
    impl<'de> serde::de::Visitor<'de> for V {
        type Value = Vec<ExchangeEntry>;
        fn expecting(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            f.write_str("a map or list of exchange entries")
        }
        fn visit_map<A: serde::de::MapAccess<'de>>(self, mut map: A) -> Result<Self::Value, A::Error> {
            let mut out = Vec::new();
            while let Some((_, v)) = map.next_entry::<serde::de::IgnoredAny, ExchangeEntry>()? {
                out.push(v);
            }
            Ok(out)
        }
        fn visit_seq<A: serde::de::SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
            let mut out = Vec::new();
            while let Some(v) = seq.next_element()? {
                out.push(v);
            }
            Ok(out)
        }
    }
    d.deserialize_any(V)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn exchange_result_accepts_map_and_empty_list() {
        let full: ExchangeResponse =
            serde_json::from_str(include_str!("../tests/fixtures/exchange.json")).expect("map form");
        assert_eq!(full.result.len(), 3);
        assert_eq!(full.result[0].listing.offers[0].item.currency, "divine");
        let empty: ExchangeResponse =
            serde_json::from_str(include_str!("../tests/fixtures/exchange.empty.json")).expect("list form");
        assert!(empty.result.is_empty());
        let bad = serde_json::from_str::<ExchangeResponse>(r#"{"id":"x","result":5,"total":0}"#);
        assert!(bad.unwrap_err().to_string().contains("map or list"));
    }
}
