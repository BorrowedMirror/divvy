//! `game-extract l10n`: the game tables the translations are built from, as plain JSON for
//! `go run ./cmd/datarefresh l10n`.
//!
//! Output: `{"folders": {<folder>: {<table>: [[column, ...], ...]}}, "stat_descriptions":
//! [{"path": ..., "text": ...}]}`. Folder `""` is English (`data/balance/<table>.datc64`),
//! the others are `data/balance/<folder>/<table>.datc64`. Each row lists the string columns
//! of [`TABLES`] in order, `null` where a column points nowhere. The stat description files
//! are decoded to text (UTF-16 or UTF-8). Everything else happens in Go.
use std::path::Path;

use anyhow::Context as _;
use serde_json::{Map, Value, json};

use crate::bundle::{Dat, GameFiles};

/// Data folders of the languages whose clients search the main trade site.
pub const FOLDERS: [&str; 8] = ["german", "french", "spanish", "portuguese", "russian", "thai", "japanese", "korean"];

/// Tables read per folder, with the byte offsets of the string columns exported.
pub const TABLES: &[(&str, &[usize])] = &[
    ("clientstrings", &[0, 8]),  // id, text
    ("clientstrings2", &[0, 8]), // id, text
    ("itemclasses", &[0, 8]),    // id, name
    ("baseitemtypes", &[0, 32]), // id, name
    ("words", &[4, 48]),         // text, localised text
];

/// The stat description files whose lines appear on items.
pub const STAT_FILES: &[&str] = &[
    "data/statdescriptions/stat_descriptions.csd",
    "data/statdescriptions/map_stat_descriptions.csd",
    "data/statdescriptions/endgame_map_stat_descriptions.csd",
    "data/statdescriptions/tablet_stat_descriptions.csd",
    "data/statdescriptions/utility_flask_buff_stat_descriptions.csd",
];

fn table_path(folder: &str, name: &str) -> String {
    if folder.is_empty() {
        format!("data/balance/{name}.datc64")
    } else {
        format!("data/balance/{folder}/{name}.datc64")
    }
}

/// The rows of a table as JSON: the strings at `columns`, `null` where there is none.
pub fn rows(dat: &Dat, columns: &[usize]) -> Value {
    (0..dat.rows)
        .map(|row| columns.iter().map(|c| dat.string(row, *c).map_or(Value::Null, Value::from)).collect::<Vec<Value>>())
        .collect()
}

/// Decode a stat description file (UTF-16 little-endian, or UTF-8).
pub fn decode_text(raw: &[u8]) -> String {
    if raw.len() >= 2 && (raw[..2] == [0xFF, 0xFE] || raw[1] == 0) {
        let units: Vec<u16> = raw.as_chunks::<2>().0.iter().map(|c| u16::from_le_bytes(*c)).collect();
        String::from_utf16_lossy(&units)
    } else {
        String::from_utf8_lossy(raw).into_owned()
    }
}

/// Read every table of `folders` (plus English) and the stat descriptions.
pub fn extract(files: &GameFiles, folders: &[String]) -> anyhow::Result<Value> {
    let mut out = Map::new();
    for folder in std::iter::once("").chain(folders.iter().map(String::as_str)) {
        let mut tables = Map::new();
        for (name, columns) in TABLES {
            let path = table_path(folder, name);
            let dat = Dat::parse(&files.read(&path)?).with_context(|| format!("parse {path}"))?;
            tables.insert((*name).to_owned(), rows(&dat, columns));
        }
        out.insert(folder.to_owned(), Value::Object(tables));
    }
    let mut descs = Vec::new();
    for path in STAT_FILES {
        descs.push(json!({"path": path, "text": decode_text(&files.read(path)?)}));
    }
    Ok(json!({"folders": out, "stat_descriptions": descs}))
}

/// `game-extract l10n <game_dir> <out.json> [folder...]`.
pub fn run(game_dir: &Path, out: &Path, folders: &[String]) -> anyhow::Result<()> {
    let files = GameFiles::open(game_dir)?;
    let folders: Vec<String> =
        if folders.is_empty() { FOLDERS.iter().map(|f| (*f).to_owned()).collect() } else { folders.to_vec() };
    let value = extract(&files, &folders)?;
    std::fs::write(out, serde_json::to_vec(&value)?).with_context(|| format!("write {}", out.display()))?;
    eprintln!("wrote {} (English and {} languages)", out.display(), folders.len());
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_utf16_and_utf8() {
        let text = "description\n\t1 armour\n\t1\n\t\t# \"{0}% erhöhte [Armour|Rüstung]\"\n";
        let utf16: Vec<u8> = std::iter::once(0xFEFFu16).chain(text.encode_utf16()).flat_map(u16::to_le_bytes).collect();
        assert_eq!(decode_text(&utf16), format!("\u{feff}{text}"));
        // Without a byte order mark, a zero second byte still means UTF-16.
        let bare: Vec<u8> = text.encode_utf16().flat_map(u16::to_le_bytes).collect();
        assert_eq!(decode_text(&bare), text);
        assert_eq!(decode_text(text.as_bytes()), text);
    }

    #[test]
    fn table_rows_as_json() {
        // Two rows of two string columns; row 1's second column points past the data.
        let mut data = 2u32.to_le_bytes().to_vec();
        for at in [8u64, 14, 20, 1000] {
            data.extend(at.to_le_bytes());
        }
        data.extend([0xBB; 8]);
        for s in ["ab", "cd", "ef"] {
            data.extend(s.encode_utf16().flat_map(u16::to_le_bytes));
            data.extend([0, 0]);
        }
        let dat = Dat::parse(&data).unwrap();
        assert_eq!(rows(&dat, &[0, 8]), json!([["ab", "cd"], ["ef", null]]));
        assert_eq!(table_path("", "words"), "data/balance/words.datc64");
        assert_eq!(table_path("german", "words"), "data/balance/german/words.datc64");
    }
}
