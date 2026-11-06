//! `cargo xtask data l10n --game-dir <dir>`: read an installed PoE2 client and write the item
//! text translations for non-English clients (`crates/data/assets/l10n.json.zst`).
//!
//! * keywords from `ClientStrings` (header keys, rarities, flags, mod-header phrases), by id;
//! * item class, base and unique names from `ItemClasses`, `BaseItemTypes` and `Words`, by row;
//! * stat lines from the stat descriptions, where each language lists its wordings in the
//!   same order as English.
//!
//! Only names and stats RKL can search for are kept (those in the other bundled assets), so
//! run this after `cargo xtask data refresh`.
use std::{
    collections::{HashMap, HashSet},
    path::Path,
    sync::LazyLock,
};

use anyhow::{Context as _, ensure};
use regex::Regex;
use rkl_core::l10n::Language;

use super::bundle::{Dat, GameFiles};

/// Languages whose clients search the main trade site: (game name, code, data folder).
pub const LANGUAGES: [(&str, &str, &str); 8] = [
    ("German", "de", "german"),
    ("French", "fr", "french"),
    ("Spanish", "es", "spanish"),
    ("Portuguese", "pt-BR", "portuguese"),
    ("Russian", "ru", "russian"),
    ("Thai", "th", "thai"),
    ("Japanese", "ja", "japanese"),
    ("Korean", "ko", "korean"),
];

/// `ClientStrings` ids per table, with the English the parser expects (checked against the
/// English client so a renamed string is caught).
const KEYS: &[(&str, &str)] = &[
    ("ItemDisplayStringClass", "Item Class"),
    ("ItemDisplayStringRarity", "Rarity"),
    ("ItemDisplayStringItemLevel", "Item Level"),
    ("ItemDisplayStringSockets", "Sockets"),
    ("ItemDisplayStackSize", "Stack Size"),
    ("ItemDisplayMapTier", "Waystone Tier"),
    ("Quality", "Quality"),
    ("Level", "Level"),
    ("ItemDisplayLakeAreaLevel", "Area Level"),
    ("ItemDisplayHeistContractLevel", "Area Level"),
    ("ItemDisplayArmourArmour", "Armour"),
    ("ItemDisplayArmourEvasionRating", "Evasion Rating"),
    ("ItemDisplayArmourEnergyShield", "Energy Shield"),
    ("ItemDisplaySpiritValue", "Spirit"),
    ("ItemDisplayShieldBlockChance", "Block chance"),
    ("ItemDisplayWeaponCriticalStrikeChance", "Critical Hit Chance"),
    ("ItemDisplayWeaponAttacksPerSecond", "Attacks per Second"),
    ("ItemDisplayWeaponPhysicalDamage", "Physical Damage"),
    ("ItemDisplayWeaponElementalDamage", "Elemental Damage"),
    ("ItemDisplayWeaponFireDamage", "Fire Damage"),
    ("ItemDisplayWeaponColdDamage", "Cold Damage"),
    ("ItemDisplayWeaponLightningDamage", "Lightning Damage"),
    ("ItemDisplayWeaponChaosDamage", "Chaos Damage"),
    ("ItemDisplayWeaponReloadTime", "Reload Time"),
    ("ItemPopupRequirements", "Requirements"),
    ("ItemRequirementsLabel", "Requires"),
    ("ItemDisplayStringNote", "Note"),
    ("ItemDisplayGrantsSkill", "Grants Skill"),
    ("StrengthShort", "Str"),
    ("DexterityShort", "Dex"),
    ("IntelligenceShort", "Int"),
    ("ItemDisplayMapWaystoneDropChance", "Waystone Drop Chance"),
    ("ItemDisplayMapPack", "Pack Size"),
    ("ItemDisplayMapItemRarity", "Item Rarity"),
    ("ItemDisplayMapMagicMonsterQuantityBonus", "Magic Monsters"),
    ("ItemDisplayMapRareMonsterQuantityBonus", "Rare Monsters"),
    ("NumberOfPortalsPerWaystone", "Revives Available"),
];

const RARITIES: &[(&str, &str)] = &[
    ("ItemDisplayStringNormal", "Normal"),
    ("ItemDisplayStringMagic", "Magic"),
    ("ItemDisplayStringRare", "Rare"),
    ("ItemDisplayStringUnique", "Unique"),
    ("ItemDisplayStringGem", "Gem"),
    ("ItemDisplayStringCurrency", "Currency"),
];

const FLAGS: &[(&str, &str)] = &[
    ("ItemPopupCorrupted", "Corrupted"),
    ("ItemPopupDoubleCorrupted", "Twice Corrupted"),
    ("ItemPopupUnidentified", "Unidentified"),
    ("ItemPopupMirrored", "Mirrored"),
    ("ItemPopupSanctified", "Sanctified"),
    ("ItemPopupFracturedItem", "Fractured Item"),
    ("ItemPopupSplit", "Split"),
    ("ItemPopupUnmodifiable", "Unmodifiable"),
    ("ItemDisplayVeiledPrefix", "Desecrated Prefix"),
    ("ItemDisplayVeiledSuffix", "Desecrated Suffix"),
];

const WORDS: &[(&str, &str)] =
    &[("Level", "Level"), ("StrengthShort", "Str"), ("DexterityShort", "Dex"), ("IntelligenceShort", "Int")];

/// Applied in order, so the specific phrases come before the generic `Fractured {0}` ones.
const PATTERNS: &[(&str, &str)] = &[
    ("QualityItem", "Superior {0}"),
    ("ModDescriptionLinePrefix", "Prefix Modifier \"{0}\""),
    ("ModDescriptionLineSuffix", "Suffix Modifier \"{0}\""),
    ("ModDescriptionLineCorruptedImplicit", "Corruption Enhancement"),
    ("ModDescriptionLineEnchantmentImplicit", "Enhancement"),
    ("ModDescriptionLineImplicit", "Implicit Modifier"),
    ("ModDescriptionLineUnique", "Unique Modifier"),
    ("ModDescriptionLineTier", " (Tier: {0})"),
    ("ModDescriptionLineRank", " (Rank: {0})"),
    ("ModDescriptionLineFractured", "Fractured {0}"),
    ("ModDescriptionLineCrafted", "Crafted {0}"),
    ("ModDescriptionLineDesecrated", "Desecrated {0}"),
];

/// The stat description files whose lines appear on items.
const STAT_FILES: &[&str] = &[
    "data/statdescriptions/stat_descriptions.csd",
    "data/statdescriptions/map_stat_descriptions.csd",
    "data/statdescriptions/endgame_map_stat_descriptions.csd",
    "data/statdescriptions/tablet_stat_descriptions.csd",
    "data/statdescriptions/utility_flask_buff_stat_descriptions.csd",
];

/// Columns (byte offsets in a row) of the tables read here.
const STRING_ID: usize = 0;
const STRING_TEXT: usize = 8;
const CLASS_ID: usize = 0;
const CLASS_NAME: usize = 8;
const BASE_ID: usize = 0;
const BASE_NAME: usize = 32;
const WORD_TEXT: usize = 4;
const WORD_LOCAL: usize = 48;

static MARKUP_PAIR: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\[[^\]|]*\|([^\]]*)\]").unwrap());
static MARKUP_ONE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\[([^\]|]*)\]").unwrap());

/// `[Evasion|Evasion Rating]` -> `Evasion Rating`, `[Armour]` -> `Armour`.
pub fn strip_markup(text: &str) -> String {
    let once = MARKUP_PAIR.replace_all(text, "$1");
    MARKUP_ONE.replace_all(&once, "$1").into_owned()
}

fn table(files: &GameFiles, folder: Option<&str>, name: &str) -> anyhow::Result<Dat> {
    let path = match folder {
        Some(f) => format!("data/balance/{f}/{name}.datc64"),
        None => format!("data/balance/{name}.datc64"),
    };
    Dat::parse(&files.read(&path)?).with_context(|| format!("parse {path}"))
}

/// `ClientStrings` (both tables) as id -> text.
fn client_strings(files: &GameFiles, folder: Option<&str>) -> anyhow::Result<HashMap<String, String>> {
    let mut out = HashMap::new();
    for name in ["clientstrings", "clientstrings2"] {
        let dat = table(files, folder, name)?;
        for row in 0..dat.rows {
            if let (Some(id), Some(text)) = (dat.string(row, STRING_ID), dat.string(row, STRING_TEXT)) {
                out.entry(id).or_insert(text);
            }
        }
    }
    Ok(out)
}

/// The text the client shows: markup stripped, a trailing `:`/space dropped for keys.
fn shown(text: &str, key: bool) -> String {
    let t = strip_markup(text);
    if key { t.trim().trim_end_matches([':', '：']).trim().to_owned() } else { t }
}

fn pick(
    en: &HashMap<String, String>,
    local: &HashMap<String, String>,
    ids: &[(&str, &str)],
    key: bool,
    lang: &str,
) -> Vec<(String, String)> {
    let mut out = Vec::new();
    for (id, expected) in ids {
        let (Some(e), Some(l)) = (en.get(*id), local.get(*id)) else {
            eprintln!("warning: {lang}: client string {id} is missing");
            continue;
        };
        let e = shown(e, key);
        if e != *expected {
            eprintln!("warning: client string {id} reads {e:?} in English, expected {expected:?}; skipped");
            continue;
        }
        let l = shown(l, key);
        if !l.is_empty() && !out.iter().any(|(a, b): &(String, String)| a == expected && *b == l) {
            out.push(((*expected).to_owned(), l));
        }
    }
    out
}

/// Pair column `col` of two copies of a table row by row (same row count and ids).
fn pair_rows(
    en: &Dat,
    local: &Dat,
    id_col: Option<usize>,
    en_col: usize,
    local_col: usize,
    keep: impl Fn(&str) -> bool,
) -> anyhow::Result<Vec<(String, String)>> {
    ensure!(en.rows == local.rows, "row counts differ: {} vs {}", en.rows, local.rows);
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    for row in 0..en.rows {
        if let Some(c) = id_col {
            ensure!(en.string(row, c) == local.string(row, c), "row {row} ids differ");
        }
        let (Some(e), Some(l)) = (en.string(row, en_col), local.string(row, local_col)) else { continue };
        if e.is_empty() || l.is_empty() || !keep(&e) {
            continue;
        }
        let l = strip_markup(&l);
        if seen.insert((e.clone(), l.clone())) {
            out.push((e, l));
        }
    }
    Ok(out)
}

/// One `description` block: its variants per language (English under `""`).
#[derive(Debug, Default)]
struct Description {
    variants: HashMap<String, Vec<String>>,
}

/// The quoted text of a variant line: `1|# "{0}% increased [Armour]" negate 1`.
fn quoted(line: &str) -> Option<String> {
    let start = line.find('"')? + 1;
    let bytes = line.as_bytes();
    let mut end = start;
    while end < bytes.len() {
        if bytes[end] == b'"' && bytes[end - 1] != b'\\' {
            break;
        }
        end += 1;
    }
    Some(line.get(start..end)?.replace("\\\"", "\"").replace("\\n", "\n"))
}

/// Parse a stat description file (UTF-16 or UTF-8 text).
fn descriptions(raw: &[u8]) -> Vec<Description> {
    let text = if raw.len() >= 2 && (raw[..2] == [0xFF, 0xFE] || raw[1] == 0) {
        let units: Vec<u16> = raw.as_chunks::<2>().0.iter().map(|c| u16::from_le_bytes(*c)).collect();
        String::from_utf16_lossy(&units)
    } else {
        String::from_utf8_lossy(raw).into_owned()
    };
    let mut out = Vec::new();
    let mut lines = text.lines().map(|l| l.trim_start_matches('\u{feff}').trim()).peekable();
    while let Some(line) = lines.next() {
        if line != "description" && !line.starts_with("description ") {
            continue;
        }
        let _ids = lines.next();
        let mut desc = Description::default();
        let mut lang = String::new();
        while let Some(count) = lines.peek().and_then(|l| l.parse::<usize>().ok()) {
            lines.next();
            let variants: Vec<String> = (0..count).filter_map(|_| lines.next()).filter_map(quoted).collect();
            desc.variants.insert(lang.clone(), variants);
            match lines.peek().and_then(|l| l.strip_prefix("lang ")) {
                Some(name) => {
                    name.trim_matches('"').clone_into(&mut lang);
                    lines.next();
                }
                None => break,
            }
        }
        out.push(desc);
    }
    out
}

static NUM_OR_HASH: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"[+-]?(?:#|\d+(?:\.\d+)?)").unwrap());
static PLACEHOLDER: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"[+-]?\{\d+(?::[^}]*)?\}").unwrap());

/// The shape both a trade template (`+# to maximum Life`) and a description
/// (`{0:+d} to maximum Life`) reduce to: every number or slot is `#`.
pub fn shape(text: &str) -> String {
    let t = PLACEHOLDER.replace_all(text, "#");
    NUM_OR_HASH.replace_all(&t, "#").into_owned()
}

/// Stat line pairs for `lang`, kept when the English wording matches a searchable stat.
fn stat_pairs(descs: &[Description], lang: &str, searchable: &HashSet<String>) -> Vec<(String, String)> {
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    for d in descs {
        let (Some(en), Some(local)) = (d.variants.get(""), d.variants.get(lang)) else { continue };
        if en.len() != local.len() {
            continue;
        }
        for (e, l) in en.iter().zip(local) {
            let (e_lines, l_lines): (Vec<String>, Vec<String>) =
                (e.split('\n').map(strip_markup).collect(), l.split('\n').map(strip_markup).collect());
            if e_lines.len() != l_lines.len() {
                continue;
            }
            for (e, l) in e_lines.into_iter().zip(l_lines) {
                if searchable.contains(&shape(&e)) && seen.insert(l.clone()) {
                    out.push((e, l));
                }
            }
        }
    }
    out
}

/// Every wording the matcher can search for, as shapes.
fn searchable_stats(bundle: &rkl_data::Bundle) -> HashSet<String> {
    let mut out = HashSet::new();
    for s in &bundle.stats {
        out.insert(shape(&s.text));
        for t in super::normalise::wording_variants(&s.text) {
            out.insert(shape(&t));
        }
        for a in &s.aliases {
            out.insert(shape(&a.text));
        }
    }
    out
}

/// Build every language's tables from the installed client.
pub fn build(game_dir: &Path, bundle: &rkl_data::Bundle) -> anyhow::Result<Vec<Language>> {
    let files = GameFiles::open(game_dir)?;
    let en_strings = client_strings(&files, None)?;
    let (en_classes, en_bases, en_words) =
        (table(&files, None, "itemclasses")?, table(&files, None, "baseitemtypes")?, table(&files, None, "words")?);
    let mut descs = Vec::new();
    for path in STAT_FILES {
        descs.extend(descriptions(&files.read(path)?));
    }
    let searchable = searchable_stats(bundle);
    let names: HashSet<&str> = bundle
        .bases
        .iter()
        .map(|b| b.name.as_str())
        .chain(bundle.statics.iter().map(|s| s.text.as_str()))
        .chain(bundle.uniques.iter().map(|u| u.base_type.as_str()))
        .collect();
    let uniques: HashSet<&str> = bundle.uniques.iter().map(|u| u.name.as_str()).collect();
    let mut out = Vec::new();
    for (name, code, folder) in LANGUAGES {
        let strings = client_strings(&files, Some(folder))?;
        let classes = pair_rows(
            &en_classes,
            &table(&files, Some(folder), "itemclasses")?,
            Some(CLASS_ID),
            CLASS_NAME,
            CLASS_NAME,
            |_| true,
        )?;
        let bases = pair_rows(
            &en_bases,
            &table(&files, Some(folder), "baseitemtypes")?,
            Some(BASE_ID),
            BASE_NAME,
            BASE_NAME,
            |n| names.contains(n),
        )?;
        let words = pair_rows(&en_words, &table(&files, Some(folder), "words")?, None, WORD_TEXT, WORD_LOCAL, |n| {
            uniques.contains(n)
        })?;
        let mut patterns = pick(&en_strings, &strings, PATTERNS, false, name);
        // `Unidentified (Tier 4)`: the flag followed by the tier suffix.
        if let (Some(u), Some(t)) = (strings.get("ItemPopupUnidentified"), strings.get("ItemTierDisplay")) {
            patterns.push(("Unidentified (Tier {0})".into(), format!("{}{}", shown(u, false), shown(t, false))));
        }
        let lang = Language {
            name: name.to_owned(),
            code: code.to_owned(),
            keys: pick(&en_strings, &strings, KEYS, true, name),
            rarities: pick(&en_strings, &strings, RARITIES, false, name),
            flags: pick(&en_strings, &strings, FLAGS, false, name),
            words: pick(&en_strings, &strings, WORDS, false, name),
            patterns,
            classes,
            bases,
            uniques: words,
            stats: stat_pairs(&descs, name, &searchable),
        };
        eprintln!(
            "{name}: {} keys, {} classes, {} bases, {} uniques, {} stat lines",
            lang.keys.len(),
            lang.classes.len(),
            lang.bases.len(),
            lang.uniques.len(),
            lang.stats.len()
        );
        out.push(lang);
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn markup_and_shapes() {
        assert_eq!(strip_markup("[Evasion|Evasion Rating]"), "Evasion Rating");
        assert_eq!(strip_markup("[Armour] and [Critical|Critical Hit] Chance"), "Armour and Critical Hit Chance");
        assert_eq!(shape("{0:+d} to maximum Life"), shape("+# to maximum Life"));
        assert_eq!(shape("{0}% increased Damage per 10 Dexterity"), "#% increased Damage per # Dexterity");
    }

    #[test]
    fn parses_descriptions_with_languages() {
        let csd = "description\n\t1 base_maximum_life\n\t1\n\t\t# \"{0:+d} to maximum Life\"\n\tlang \"German\"\n\t1\n\t\t# \"{0:+d} zu maximalem Leben\"\n\ndescription\n\t1 armour\n\t2\n\t\t1|# \"{0}% increased [Armour]\"\n\t\t#|-1 \"{0}% reduced [Armour]\" negate 1\n\tlang \"German\"\n\t2\n\t\t1|# \"{0}% erhöhte [Armour|Rüstung]\"\n\t\t#|-1 \"{0}% verringerte [Armour|Rüstung]\" negate 1\n";
        let descs = descriptions(csd.as_bytes());
        assert_eq!(descs.len(), 2);
        let searchable: HashSet<String> =
            ["# to maximum Life", "#% increased Armour", "#% reduced Armour"].iter().map(|s| shape(s)).collect();
        let pairs = stat_pairs(&descs, "German", &searchable);
        assert_eq!(
            pairs,
            vec![
                ("{0:+d} to maximum Life".to_owned(), "{0:+d} zu maximalem Leben".to_owned()),
                ("{0}% increased Armour".to_owned(), "{0}% erhöhte Rüstung".to_owned()),
                ("{0}% reduced Armour".to_owned(), "{0}% verringerte Rüstung".to_owned()),
            ]
        );
        // UTF-16 input parses the same.
        let utf16: Vec<u8> = std::iter::once(0xFEFFu16).chain(csd.encode_utf16()).flat_map(u16::to_le_bytes).collect();
        assert_eq!(descriptions(&utf16).len(), 2);
    }

    #[test]
    fn quoted_text_unescapes() {
        assert_eq!(quoted(r#"1|# "Prefix \"{0}\"" negate 1"#).as_deref(), Some("Prefix \"{0}\""));
        assert_eq!(quoted("no quotes"), None);
    }
}
