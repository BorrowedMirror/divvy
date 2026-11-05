use serde::{Deserialize, Serialize};

/// Errors produced while parsing clipboard item text.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error, Serialize, Deserialize, specta::Type)]
#[serde(tag = "kind", content = "detail", rename_all = "snake_case")]
pub enum ParseError {
    /// Input was empty or whitespace only.
    #[error("item text is empty")]
    Empty,
    /// Input did not start with an `Item Class:` header (not a PoE2 item copy).
    #[error("not a Path of Exile 2 item: missing `Item Class:` header")]
    NotAnItem,
    /// The rarity line was missing or held an unknown value.
    #[error("unknown rarity `{0}`")]
    UnknownRarity(String),
    /// Item class header held a value we cannot map.
    #[error("unknown item class `{0}`")]
    UnknownItemClass(String),
    /// Structural problem with a named section.
    #[error("malformed section `{section}`: {reason}")]
    Malformed { section: String, reason: String },
}

/// Umbrella error for core operations other than parsing.
#[derive(Debug, Clone, PartialEq, thiserror::Error, Serialize, Deserialize, specta::Type)]
#[serde(tag = "kind", content = "detail", rename_all = "snake_case")]
pub enum CoreError {
    #[error(transparent)]
    Parse(#[from] ParseError),
    /// The item class cannot be priced via the requested route.
    #[error("item cannot be priced: {0}")]
    Unpriceable(String),
}
