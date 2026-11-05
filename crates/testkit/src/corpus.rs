//! Corpus format
//! =============
//! `corpus/index.toml` lists entries:
//! ```toml
//! [[item]]
//! id = "currency/divine-orb"         # path under corpus/ without extension
//! class = "StackableCurrency"        # expected ItemClass variant name
//! rarity = "currency"                # expected Rarity (snake_case)
//! advanced = false                   # copied with Ctrl+Alt+C
//! source = "hand-written from 0.3 in-game copy"
//! ```
//! Each entry has `corpus/<id>.txt` containing the exact clipboard text (LF line endings;
//! the parser must also accept CRLF). Expected parse output is an insta snapshot in
//! `crates/core/tests/snapshots/`.
use std::path::{Path, PathBuf};

use serde::Deserialize;

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct CorpusEntry {
    pub id: String,
    pub class: String,
    pub rarity: String,
    #[serde(default)]
    pub advanced: bool,
    #[serde(default)]
    pub source: Option<String>,
}

#[derive(Debug, Deserialize)]
struct Index {
    #[serde(default)]
    item: Vec<CorpusEntry>,
}

pub fn corpus_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("corpus")
}

/// All corpus entries with their text.
pub fn load() -> Vec<(CorpusEntry, String)> {
    let dir = corpus_dir();
    let index: Index = toml::from_str(&std::fs::read_to_string(dir.join("index.toml")).expect("corpus index"))
        .expect("valid corpus index");
    index
        .item
        .into_iter()
        .map(|e| {
            let text = std::fs::read_to_string(dir.join(format!("{}.txt", e.id))).expect("corpus item text");
            (e, text)
        })
        .collect()
}

/// One entry by id.
pub fn get(id: &str) -> String {
    std::fs::read_to_string(corpus_dir().join(format!("{id}.txt"))).expect("corpus item")
}

#[cfg(test)]
mod tests {
    #[test]
    fn corpus_loads() {
        let items = super::load();
        assert!(!items.is_empty());
        for (e, text) in items {
            assert!(text.starts_with("Item Class:"), "{} must start with Item Class:", e.id);
        }
    }
}
