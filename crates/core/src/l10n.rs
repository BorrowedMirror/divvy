//! Item text copied from a non-English game client, translated to the English text the parser
//! and matcher understand. Trade searches use language-neutral stat ids and English names, so
//! everything after this step works unchanged.
//!
//! The tables come from the game's own data (`cargo xtask data l10n`): keywords from its
//! client strings, item class, base and unique names by row, and every wording of every stat
//! from its stat descriptions, where each language lists its variants in the same order as
//! English.
use std::collections::HashMap;
use std::sync::LazyLock;

use regex::Regex;
use serde::{Deserialize, Serialize};

/// One game language's tables, as (English, localized) pairs.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Language {
    /// The game's name for the language, e.g. `German`.
    pub name: String,
    /// Short code, e.g. `de`.
    pub code: String,
    /// `Key: value` line keys (`Item Class`, `Rarity`, `Item Level`, `Armour`, ...).
    pub keys: Vec<(String, String)>,
    /// Rarity values (`Rare`, `Unique`, ...).
    pub rarities: Vec<(String, String)>,
    /// Whole-line flags (`Corrupted`, `Unidentified`, `Desecrated Prefix`, ...).
    pub flags: Vec<(String, String)>,
    /// Single words in a `Requires:` line (`Level`, `Str`, `Dex`, `Int`).
    pub words: Vec<(String, String)>,
    /// Patterns with `{0}`, replaced inside a line: mod headers (`Prefix Modifier "{0}"`,
    /// ` (Tier: {0})`), `Superior {0}`, `Unidentified (Tier {0})`.
    pub patterns: Vec<(String, String)>,
    pub classes: Vec<(String, String)>,
    pub bases: Vec<(String, String)>,
    pub uniques: Vec<(String, String)>,
    /// Stat lines with indexed placeholders (`{0:+d} to maximum Life`); literal numbers are
    /// kept as text.
    pub stats: Vec<(String, String)>,
}

/// A text that was translated, and from which language.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Translated {
    pub text: String,
    /// [`Language::name`] of the client the text came from.
    pub language: String,
}

/// Lines starting with this are section separators.
const SEPARATOR: &str = "--------";

/// A number as it appears on an item: sign, decimals (`.` or `,`), and an advanced-copy roll
/// range right after it (`42(40-49)`).
static NUMBER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[+-]?\d+(?:[.,]\d+)?(?:\([+-]?\d+(?:[.,]\d+)?-[+-]?\d+(?:[.,]\d+)?\))?").unwrap());
/// A placeholder in a stat template: `{0}`, `{1:+d}`, `{0:d}`; a sign written just before it
/// belongs to the number.
static PLACEHOLDER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[+-]?\{(\d+)(?::([^}]*))?\}|[+-]?\d+(?:\.\d+)?").unwrap());
/// Lower-case markers the client appends to mod lines in every language: ` (implicit)`.
static MARKER: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(?: \([a-z]+\))+$").unwrap());

/// One numeric slot of a template, in order: a placeholder index (`Some`) or a literal number.
type Slots = Vec<Option<usize>>;

#[derive(Debug, Clone)]
enum Seg {
    Text(String),
    /// Placeholder index, and whether the format asks for a sign (`+d`).
    Value(usize, bool),
}

#[derive(Debug, Clone)]
struct StatPair {
    english: Vec<Seg>,
    local: Slots,
}

/// `{0:+d} zu maximalem Leben` -> (`# zu maximalem Leben`, slots).
fn template_shape(template: &str) -> (String, Slots) {
    let mut slots = Vec::new();
    let shape = PLACEHOLDER.replace_all(template, |c: &regex::Captures<'_>| {
        slots.push(c.get(1).and_then(|m| m.as_str().parse().ok()));
        "#"
    });
    (shape.into_owned(), slots)
}

/// `+92 zu maximalem Leben` -> (`# zu maximalem Leben`, [`+92`]).
fn line_shape(line: &str) -> (String, Vec<&str>) {
    let tokens = NUMBER.find_iter(line).map(|m| m.as_str()).collect();
    (NUMBER.replace_all(line, "#").into_owned(), tokens)
}

fn english_segments(template: &str) -> Vec<Seg> {
    let mut out = Vec::new();
    let mut last = 0;
    for c in PLACEHOLDER.captures_iter(template) {
        let m = c.get(0).expect("match");
        // A literal number stays text.
        let Some(index) = c.get(1).and_then(|i| i.as_str().parse().ok()) else { continue };
        out.push(Seg::Text(template[last..m.start()].to_owned()));
        let signed = c.get(2).is_some_and(|f| f.as_str().contains('+')) || m.as_str().starts_with('+');
        out.push(Seg::Value(index, signed));
        last = m.end();
    }
    out.push(Seg::Text(template[last..].to_owned()));
    out
}

/// A number from the item, written the English way: `,` decimals become `.`, and a value the
/// English template signs gets its `+`.
fn english_number(token: &str, signed: bool) -> String {
    let mut t = token.to_owned();
    if let Some(i) = t.find(',')
        && t[i + 1..].chars().next().is_some_and(|c| c.is_ascii_digit())
    {
        t = t.replace(',', ".");
    }
    if signed && !t.starts_with(['+', '-']) {
        t.insert(0, '+');
    }
    t
}

#[derive(Debug, Clone)]
struct Compiled {
    name: String,
    keys: HashMap<String, String>,
    rarities: HashMap<String, String>,
    flags: HashMap<String, String>,
    words: HashMap<String, String>,
    patterns: Vec<(Regex, String)>,
    classes: HashMap<String, String>,
    bases: HashMap<String, String>,
    /// Localized base names, longest first, for magic items (`<prefix> <base> <suffix>`).
    bases_longest: Vec<(String, String)>,
    uniques: HashMap<String, String>,
    stats: HashMap<String, StatPair>,
    class_key: Option<String>,
    rarity_key: Option<String>,
}

fn invert(pairs: &[(String, String)]) -> HashMap<String, String> {
    let mut map = HashMap::with_capacity(pairs.len());
    for (en, local) in pairs {
        map.entry(local.clone()).or_insert_with(|| en.clone());
    }
    map
}

/// `Präfix-Modifikator "{0}"` -> a regex finding it in a line, and `Prefix Modifier "$1"`.
fn pattern(en: &str, local: &str) -> Option<(Regex, String)> {
    let Some((before, after)) = local.split_once("{0}") else {
        // A fixed phrase (`Implicit Modifier`).
        return Some((Regex::new(&regex::escape(local)).ok()?, en.replace('$', "$$")));
    };
    let lazy = if after.is_empty() { "(.+)" } else { "(.+?)" };
    let re = Regex::new(&format!("{}{lazy}{}", regex::escape(before), regex::escape(after))).ok()?;
    Some((re, en.replace('$', "$$").replace("{0}", "${1}")))
}

impl Compiled {
    fn new(lang: &Language) -> Self {
        let mut stats = HashMap::with_capacity(lang.stats.len());
        for (en, local) in &lang.stats {
            let (shape, slots) = template_shape(local);
            stats.entry(shape).or_insert_with(|| StatPair { english: english_segments(en), local: slots });
        }
        let mut bases_longest: Vec<(String, String)> =
            lang.bases.iter().map(|(en, local)| (local.clone(), en.clone())).collect();
        bases_longest.sort_by_key(|(local, _)| std::cmp::Reverse(local.chars().count()));
        let key = |en: &str| lang.keys.iter().find(|(e, _)| e == en).map(|(_, l)| l.clone());
        Self {
            name: lang.name.clone(),
            keys: invert(&lang.keys),
            rarities: invert(&lang.rarities),
            flags: invert(&lang.flags),
            words: invert(&lang.words),
            patterns: lang.patterns.iter().filter_map(|(en, local)| pattern(en, local)).collect(),
            classes: invert(&lang.classes),
            bases: invert(&lang.bases),
            bases_longest,
            uniques: invert(&lang.uniques),
            stats,
            class_key: key("Item Class"),
            rarity_key: key("Rarity"),
        }
    }

    fn apply_patterns(&self, line: &str) -> String {
        let mut out = line.to_owned();
        for (re, en) in &self.patterns {
            out = re.replace_all(&out, en.as_str()).into_owned();
        }
        out
    }

    fn base(&self, name: &str) -> Option<String> {
        if let Some(en) = self.bases.get(name) {
            return Some(en.clone());
        }
        // `Superior Base` is a pattern (`{0} (hochwertig)` in German).
        let unwrapped = self.apply_patterns(name);
        if unwrapped != name {
            let inner = unwrapped.strip_prefix("Superior ").unwrap_or(&unwrapped);
            if let Some(en) = self.bases.get(inner) {
                return Some(format!("Superior {en}"));
            }
        }
        // A magic item's name wraps its base in affixes.
        self.bases_longest.iter().find(|(local, _)| name.contains(local.as_str())).map(|(_, en)| en.clone())
    }

    fn stat(&self, line: &str) -> Option<String> {
        let (shape, tokens) = line_shape(line);
        let pair = self.stats.get(&shape)?;
        if tokens.len() != pair.local.len() {
            return None;
        }
        let mut values: HashMap<usize, &str> = HashMap::new();
        for (slot, token) in pair.local.iter().zip(&tokens) {
            if let Some(i) = slot {
                values.insert(*i, token);
            }
        }
        let mut out = String::new();
        for seg in &pair.english {
            match seg {
                Seg::Text(t) => out.push_str(t),
                Seg::Value(i, signed) => out.push_str(&english_number(values.get(i)?, *signed)),
            }
        }
        Some(out)
    }

    /// `Key: value` -> its English form, or `None` when the key is not one we know.
    fn header(&self, line: &str) -> Option<String> {
        let (key, value) = line.split_once(':').or_else(|| line.split_once('：'))?;
        let en = self.keys.get(key.trim())?;
        let value = value.trim();
        let value = match en.as_str() {
            "Item Class" => self.classes.get(value).cloned().unwrap_or_else(|| value.to_owned()),
            "Rarity" => self.rarities.get(value).cloned().unwrap_or_else(|| value.to_owned()),
            "Requires" => value
                .split(',')
                .map(|part| {
                    part.split_whitespace()
                        .map(|w| self.words.get(w).map_or(w, String::as_str))
                        .collect::<Vec<_>>()
                        .join(" ")
                })
                .collect::<Vec<_>>()
                .join(", "),
            _ => value.to_owned(),
        };
        Some(if value.is_empty() { format!("{en}:") } else { format!("{en}: {value}") })
    }

    fn line(&self, line: &str) -> String {
        if let Some(h) = self.header(line) {
            return h;
        }
        if let Some(f) = self.flags.get(line) {
            return f.clone();
        }
        if line.starts_with('{') {
            return self.apply_patterns(line);
        }
        let (body, marker) = match MARKER.find(line) {
            Some(m) => (&line[..m.start()], m.as_str()),
            None => (line, ""),
        };
        // ` — Unscalable Value` and similar tails in advanced copies stay as they are.
        let (body, tail) = match body.find(" — ") {
            Some(i) => (&body[..i], &body[i..]),
            None => (body, ""),
        };
        if let Some(en) = self.stat(body) {
            return format!("{en}{tail}{marker}");
        }
        let patterned = self.apply_patterns(line);
        if patterned != line {
            return patterned;
        }
        line.to_owned()
    }

    fn translate(&self, text: &str) -> String {
        let mut out = Vec::new();
        let mut rarity = None;
        // Name lines: after the header, before the first separator.
        let mut in_names = false;
        let mut names: Vec<&str> = Vec::new();
        let flush = |names: &mut Vec<&str>, rarity: Option<&str>, out: &mut Vec<String>| {
            let n = names.len();
            for (i, name) in names.drain(..).enumerate() {
                let is_base = i + 1 == n;
                let en = if is_base {
                    self.base(name)
                } else if rarity == Some("Unique") {
                    self.uniques.get(name).cloned()
                } else {
                    None
                };
                out.push(en.unwrap_or_else(|| name.to_owned()));
            }
        };
        for raw in text.lines() {
            let line = raw.trim();
            if line.starts_with(SEPARATOR) {
                if in_names {
                    flush(&mut names, rarity.as_deref(), &mut out);
                    in_names = false;
                }
                out.push(line.to_owned());
                continue;
            }
            if in_names {
                if !line.is_empty() {
                    names.push(line);
                }
                continue;
            }
            let translated = self.line(line);
            if let Some(r) = translated.strip_prefix("Rarity: ") {
                rarity = Some(r.to_owned());
                in_names = true;
            }
            out.push(translated);
        }
        if in_names {
            flush(&mut names, rarity.as_deref(), &mut out);
        }
        out.join("\n")
    }

    /// The first line's key is this language's `Item Class` (or `Rarity`, for gems).
    fn detects(&self, first_key: &str) -> bool {
        self.class_key.as_deref() == Some(first_key) || self.rarity_key.as_deref() == Some(first_key)
    }
}

/// Translates item text from any of the languages it was built with.
#[derive(Debug, Clone, Default)]
pub struct Translator {
    languages: Vec<Compiled>,
}

impl Translator {
    pub fn new(languages: &[Language]) -> Self {
        Self { languages: languages.iter().map(Compiled::new).collect() }
    }

    /// The text in English, when it came from one of the known non-English clients; `None`
    /// for English (or unrecognised) text.
    pub fn translate(&self, text: &str) -> Option<Translated> {
        let first = text.lines().map(str::trim).find(|l| !l.is_empty())?;
        let (key, _) = first.split_once(':').or_else(|| first.split_once('：'))?;
        let key = key.trim();
        if key == "Item Class" || key == "Rarity" {
            return None;
        }
        let lang = self.languages.iter().find(|l| l.detects(key))?;
        Some(Translated { text: lang.translate(text), language: lang.name.clone() })
    }

    pub fn is_empty(&self) -> bool {
        self.languages.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pairs(p: &[(&str, &str)]) -> Vec<(String, String)> {
        p.iter().map(|(a, b)| ((*a).to_owned(), (*b).to_owned())).collect()
    }

    fn german() -> Language {
        Language {
            name: "German".into(),
            code: "de".into(),
            keys: pairs(&[
                ("Item Class", "Gegenstandsklasse"),
                ("Rarity", "Seltenheit"),
                ("Item Level", "Gegenstandsstufe"),
                ("Requires", "Erfordert"),
                ("Armour", "Rüstung"),
            ]),
            rarities: pairs(&[("Rare", "Selten"), ("Unique", "Einzigartig"), ("Normal", "Normal")]),
            flags: pairs(&[("Corrupted", "Verderbt")]),
            words: pairs(&[("Level", "Stufe"), ("Dex", "Ges")]),
            patterns: pairs(&[
                ("Superior {0}", "{0} (hochwertig)"),
                ("Prefix Modifier \"{0}\"", "Präfix-Modifikator \"{0}\""),
                (" (Tier: {0})", " (Level: {0})"),
            ]),
            classes: pairs(&[("Rings", "Ringe"), ("Helmets", "Helme")]),
            bases: pairs(&[("Sapphire Ring", "Saphirring"), ("Iron Hat", "Eisenhut")]),
            uniques: pairs(&[("Headhunter", "Kopfjäger")]),
            stats: pairs(&[
                ("{0:+d} to maximum Life", "{0:+d} zu maximalem Leben"),
                ("{0}% increased Armour", "{0}% erhöhte Rüstung"),
                ("{0}% reduced Armour", "{0}% verringerte Rüstung"),
                ("Adds {0} to {1} Fire Damage", "Fügt {0} bis {1} Feuerschaden hinzu"),
                ("{0}% faster start of Energy Shield Recharge", "Energieschild-Aufladung beginnt {0}% schneller"),
                ("{0:+d}% to Cold Resistance", "{0:+d}% zu Kältewiderstand"),
            ]),
        }
    }

    #[test]
    fn translates_a_rare_item() {
        let t = Translator::new(&[german()]);
        let text = "Gegenstandsklasse: Ringe\nSeltenheit: Selten\nGale Loop\nSaphirring\n--------\nErfordert: Stufe 40, 13 Ges\n--------\nGegenstandsstufe: 75\n--------\n+20% zu Kältewiderstand (implicit)\n--------\n+52 zu maximalem Leben\n10% verringerte Rüstung\nFügt 3 bis 7 Feuerschaden hinzu\nEnergieschild-Aufladung beginnt 12,5% schneller\nEtwas Unbekanntes\n--------\nVerderbt";
        let out = t.translate(text).unwrap();
        assert_eq!(out.language, "German");
        assert_eq!(
            out.text,
            "Item Class: Rings\nRarity: Rare\nGale Loop\nSapphire Ring\n--------\nRequires: Level 40, 13 Dex\n--------\nItem Level: 75\n--------\n+20% to Cold Resistance (implicit)\n--------\n+52 to maximum Life\n10% reduced Armour\nAdds 3 to 7 Fire Damage\n12.5% faster start of Energy Shield Recharge\nEtwas Unbekanntes\n--------\nCorrupted"
        );
    }

    #[test]
    fn names_superior_magic_unique_and_advanced_headers() {
        let t = Translator::new(&[german()]);
        let normal = t
            .translate("Gegenstandsklasse: Helme\nSeltenheit: Normal\nEisenhut (hochwertig)\n--------\nRüstung: 12")
            .unwrap();
        assert_eq!(normal.text, "Item Class: Helmets\nRarity: Normal\nSuperior Iron Hat\n--------\nArmour: 12");
        let magic =
            t.translate("Gegenstandsklasse: Ringe\nSeltenheit: Magisch\nFunkelnder Saphirring des Fuchses").unwrap();
        assert!(magic.text.ends_with("\nSapphire Ring"), "{}", magic.text);
        let unique = t.translate("Gegenstandsklasse: Ringe\nSeltenheit: Einzigartig\nKopfjäger\nSaphirring").unwrap();
        assert!(unique.text.ends_with("Rarity: Unique\nHeadhunter\nSapphire Ring"), "{}", unique.text);
        let adv = t
            .translate("Gegenstandsklasse: Ringe\nSeltenheit: Selten\nX\nSaphirring\n--------\n{ Präfix-Modifikator \"Gesund\" (Level: 7) — Leben }\n+42(40-49) zu maximalem Leben")
            .unwrap();
        assert!(
            adv.text.ends_with("{ Prefix Modifier \"Gesund\" (Tier: 7) — Leben }\n+42(40-49) to maximum Life"),
            "{}",
            adv.text
        );
    }

    #[test]
    fn english_and_unknown_text_are_left_alone() {
        let t = Translator::new(&[german()]);
        assert_eq!(t.translate("Item Class: Rings\nRarity: Rare\nX\nSapphire Ring"), None);
        assert_eq!(t.translate("Rarity: Gem\nCoil"), None);
        assert_eq!(t.translate("hello there"), None);
        assert_eq!(t.translate(""), None);
    }

    #[test]
    fn reordered_placeholders_keep_their_values() {
        let lang = Language {
            name: "Japanese".into(),
            code: "ja".into(),
            keys: pairs(&[("Item Class", "アイテムクラス"), ("Rarity", "レアリティ")]),
            stats: pairs(&[("Adds {0} to {1} Fire Damage", "{1}から{0}の火ダメージを追加する")]),
            ..Language::default()
        };
        let t = Translator::new(&[lang]);
        let out = t.translate("アイテムクラス: 指輪\n--------\n7から3の火ダメージを追加する").unwrap();
        assert!(out.text.ends_with("Adds 3 to 7 Fire Damage"), "{}", out.text);
    }
}
