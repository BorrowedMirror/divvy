//! rkl-data: bundled game data.
//!
//! Assets live in `crates/data/assets/` as zstd-compressed JSON (`*.json.zst`) plus a
//! plain `manifest.json` ([`Manifest`]) recording schema version, source URLs and sha256
//! of each decompressed file. Assets are produced by `cargo xtask data refresh`.
use std::{path::Path, sync::OnceLock};

use rkl_core::catalog::{BaseDef, StatDef, StaticDef, UniqueDef};
use rkl_core::l10n::Language;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

mod catalog;
pub use catalog::GameData;

/// Current asset schema version. Bump on breaking asset format change.
pub const SCHEMA_VERSION: u32 = 1;

/// File name of the manifest within an asset directory.
pub const MANIFEST_FILE: &str = "manifest.json";

/// zstd level used by [`encode_asset`]. High because assets are generated rarely and
/// shipped in every binary.
const ZSTD_LEVEL: i32 = 19;

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
    /// Item text translations for non-English game clients (`cargo xtask data l10n`), when
    /// generated.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub l10n: Option<AssetEntry>,
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
    /// Reading an asset file from disk failed (e.g. missing file in [`load_dir`]).
    #[error("asset `{0}` could not be read: {1}")]
    Io(String, std::io::Error),
}

/// Raw decoded asset bundle.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Bundle {
    pub stats: Vec<StatDef>,
    pub bases: Vec<BaseDef>,
    pub uniques: Vec<UniqueDef>,
    pub statics: Vec<StaticDef>,
    #[serde(default)]
    pub languages: Vec<Language>,
}

/// Lowercase hex sha256 of `bytes`.
pub fn sha256_hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    Sha256::digest(bytes).iter().fold(String::with_capacity(64), |mut s, b| {
        let _ = write!(s, "{b:02x}");
        s
    })
}

/// Decode and verify one compressed asset.
pub fn decode_asset<T: serde::de::DeserializeOwned>(entry: &AssetEntry, compressed: &[u8]) -> Result<T, DataError> {
    let json = zstd::decode_all(compressed).map_err(|e| DataError::Decompress(entry.file.clone(), e))?;
    let actual = sha256_hex(&json);
    if !actual.eq_ignore_ascii_case(&entry.sha256) {
        return Err(DataError::Hash { file: entry.file.clone(), expected: entry.sha256.clone(), actual });
    }
    serde_json::from_slice(&json).map_err(|e| DataError::Json(entry.file.clone(), e))
}

/// Encode `records` as an asset: compact JSON, zstd-compressed, with a matching
/// [`AssetEntry`]. Inverse of [`decode_asset`]; used by `xtask data refresh` and tests.
pub fn encode_asset<T: Serialize>(file: &str, sources: Vec<String>, records: &[T]) -> (AssetEntry, Vec<u8>) {
    let json = serde_json::to_vec(records).expect("asset records serialise to JSON");
    let compressed = zstd::encode_all(json.as_slice(), ZSTD_LEVEL).expect("in-memory zstd encode");
    let entry = AssetEntry { file: file.to_owned(), sources, sha256: sha256_hex(&json), records: records.len() };
    (entry, compressed)
}

/// Parse a manifest and check its schema version.
pub fn parse_manifest(bytes: &[u8]) -> Result<Manifest, DataError> {
    let manifest: Manifest = serde_json::from_slice(bytes).map_err(|e| DataError::Json(MANIFEST_FILE.to_owned(), e))?;
    if manifest.schema_version != SCHEMA_VERSION {
        return Err(DataError::Schema(manifest.schema_version));
    }
    Ok(manifest)
}

/// Build [`GameData`] from a manifest and a function returning the compressed bytes of
/// each asset file.
fn load_with<'a>(
    manifest_bytes: &[u8],
    mut read: impl FnMut(&str) -> Result<std::borrow::Cow<'a, [u8]>, DataError>,
) -> Result<GameData, DataError> {
    let manifest = parse_manifest(manifest_bytes)?;
    let bundle = Bundle {
        stats: decode_asset(&manifest.stats, &read(&manifest.stats.file)?)?,
        bases: decode_asset(&manifest.bases, &read(&manifest.bases.file)?)?,
        uniques: decode_asset(&manifest.uniques, &read(&manifest.uniques.file)?)?,
        statics: decode_asset(&manifest.statics, &read(&manifest.statics.file)?)?,
        languages: match &manifest.l10n {
            Some(entry) => decode_asset(entry, &read(&entry.file)?)?,
            None => Vec::new(),
        },
    };
    Ok(GameData::from_bundle(manifest, bundle))
}

mod embedded {
    pub const MANIFEST: &[u8] = include_bytes!("../assets/manifest.json");
    pub const FILES: [(&str, &[u8]); 5] = [
        ("stats.json.zst", include_bytes!("../assets/stats.json.zst")),
        ("bases.json.zst", include_bytes!("../assets/bases.json.zst")),
        ("uniques.json.zst", include_bytes!("../assets/uniques.json.zst")),
        ("statics.json.zst", include_bytes!("../assets/statics.json.zst")),
        ("l10n.json.zst", include_bytes!("../assets/l10n.json.zst")),
    ];
}

fn decode_embedded(manifest: &[u8], files: &[(&str, &'static [u8])]) -> Result<GameData, DataError> {
    load_with(manifest, |name| {
        files
            .iter()
            .find(|(n, _)| *n == name)
            .map(|(_, b)| std::borrow::Cow::Borrowed(*b))
            .ok_or_else(|| DataError::Io(name.to_owned(), std::io::ErrorKind::NotFound.into()))
    })
}

static BUNDLED: OnceLock<GameData> = OnceLock::new();

/// Load the assets compiled into the binary (`include_bytes!`).
///
/// Decoding happens once per process; later calls return a cheap clone (the indexed data
/// is shared behind an `Arc`).
pub fn bundled() -> Result<GameData, DataError> {
    if let Some(data) = BUNDLED.get() {
        return Ok(data.clone());
    }
    let data = decode_embedded(embedded::MANIFEST, &embedded::FILES)?;
    Ok(BUNDLED.get_or_init(|| data).clone())
}

/// The bundled assets as a process-wide static.
///
/// # Panics
/// If the compiled-in assets are corrupt; this is a build defect caught by this crate's
/// tests.
pub fn bundled_static() -> &'static GameData {
    if BUNDLED.get().is_none() {
        let _ = bundled().expect("bundled game data assets are valid");
    }
    BUNDLED.get().expect("initialised above")
}

/// Load from an explicit directory (manifest + assets), for tests and updates.
pub fn load_dir(dir: &Path) -> Result<GameData, DataError> {
    let manifest = std::fs::read(dir.join(MANIFEST_FILE)).map_err(|e| DataError::Io(MANIFEST_FILE.to_owned(), e))?;
    load_with(&manifest, |name| {
        std::fs::read(dir.join(name)).map(std::borrow::Cow::Owned).map_err(|e| DataError::Io(name.to_owned(), e))
    })
}

/// Write a bundle as an asset directory (manifest + `*.json.zst`), returning the manifest.
/// Used by `xtask data refresh` and tests; records are written in the given order.
pub fn write_dir(
    dir: &Path,
    bundle: &Bundle,
    sources: &AssetSources,
    generated_at: String,
    game_version: Option<String>,
) -> Result<Manifest, DataError> {
    std::fs::create_dir_all(dir).map_err(|e| DataError::Io(dir.display().to_string(), e))?;
    let write = |name: &str, entry_bytes: (AssetEntry, Vec<u8>)| -> Result<AssetEntry, DataError> {
        std::fs::write(dir.join(name), &entry_bytes.1).map_err(|e| DataError::Io(name.to_owned(), e))?;
        Ok(entry_bytes.0)
    };
    let manifest = Manifest {
        schema_version: SCHEMA_VERSION,
        generated_at,
        game_version,
        stats: write("stats.json.zst", encode_asset("stats.json.zst", sources.stats.clone(), &bundle.stats))?,
        bases: write("bases.json.zst", encode_asset("bases.json.zst", sources.bases.clone(), &bundle.bases))?,
        uniques: write("uniques.json.zst", encode_asset("uniques.json.zst", sources.uniques.clone(), &bundle.uniques))?,
        statics: write("statics.json.zst", encode_asset("statics.json.zst", sources.statics.clone(), &bundle.statics))?,
        l10n: if bundle.languages.is_empty() {
            None
        } else {
            Some(write("l10n.json.zst", encode_asset("l10n.json.zst", sources.l10n.clone(), &bundle.languages))?)
        },
    };
    let mut json = serde_json::to_vec_pretty(&manifest).expect("manifest serialises");
    json.push(b'\n');
    std::fs::write(dir.join(MANIFEST_FILE), json).map_err(|e| DataError::Io(MANIFEST_FILE.to_owned(), e))?;
    Ok(manifest)
}

/// Upstream source URLs per asset, recorded in the manifest by [`write_dir`].
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct AssetSources {
    pub stats: Vec<String>,
    pub bases: Vec<String>,
    pub uniques: Vec<String>,
    pub statics: Vec<String>,
    pub l10n: Vec<String>,
}

#[cfg(test)]
mod tests;
