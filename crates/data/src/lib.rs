//! kalandra-data: bundled game data.
//!
//! Assets live in `crates/data/assets/` as zstd-compressed JSON (`*.json.zst`) plus a
//! plain `manifest.json` ([`Manifest`]) recording schema version, source URLs and sha256
//! of each decompressed file. Assets are produced by `cargo xtask data refresh`.
use kalandra_core::catalog::{BaseDef, StatDef, StaticDef, UniqueDef};
use serde::{Deserialize, Serialize};

mod catalog;
pub use catalog::GameData;

/// Current asset schema version. Bump on breaking asset format change.
pub const SCHEMA_VERSION: u32 = 1;

/// One asset file entry in the manifest.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AssetEntry {
    /// File name within `assets/`, e.g. `stats.json.zst`.
    pub file: String,
    /// Upstream URL(s) the data was derived from.
    pub sources: Vec<String>,
    /// Lowercase hex sha256 of the decompressed JSON.
    pub sha256: String,
    pub records: usize,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Manifest {
    pub schema_version: u32,
    /// RFC 3339 generation time.
    pub generated_at: String,
    /// Game patch / data version the assets were generated for, if known.
    pub game_version: Option<String>,
    pub stats: AssetEntry,
    pub bases: AssetEntry,
    pub uniques: AssetEntry,
    pub statics: AssetEntry,
}

#[derive(Debug, thiserror::Error)]
pub enum DataError {
    #[error("asset `{0}` failed to decompress: {1}")]
    Decompress(String, std::io::Error),
    #[error("asset `{0}` is not valid JSON: {1}")]
    Json(String, serde_json::Error),
    #[error("asset `{file}` hash mismatch: expected {expected}, got {actual}")]
    Hash { file: String, expected: String, actual: String },
    #[error("unsupported schema version {0} (expected {SCHEMA_VERSION})")]
    Schema(u32),
}

/// Raw decoded asset bundle.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Bundle {
    pub stats: Vec<StatDef>,
    pub bases: Vec<BaseDef>,
    pub uniques: Vec<UniqueDef>,
    pub statics: Vec<StaticDef>,
}

/// Decode and verify one compressed asset.
pub fn decode_asset<T: serde::de::DeserializeOwned>(_entry: &AssetEntry, _compressed: &[u8]) -> Result<T, DataError> {
    todo!("workstream C")
}

/// Load the assets compiled into the binary (`include_bytes!`).
pub fn bundled() -> Result<GameData, DataError> {
    todo!("workstream C")
}

/// Load from an explicit directory (manifest + assets), for tests and updates.
pub fn load_dir(_dir: &std::path::Path) -> Result<GameData, DataError> {
    todo!("workstream C")
}
