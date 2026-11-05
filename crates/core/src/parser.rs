//! Parser for PoE2 clipboard item text (Ctrl+C and Ctrl+Alt+C formats).
//!
//! Sections are separated by lines of `--------`. The first section is the header
//! (`Item Class:`, `Rarity:`, name lines). Meta gems omit `Item Class:` and start at
//! `Rarity: Gem`.
//!
//! Every later section is classified by its first line:
//! * flag sections (`Corrupted`, `Unidentified`, `Mirrored`, `Sanctified`, ...);
//! * `Note:`, `Requires:` / `Requirements:`, `Sockets:`, `Item Level:`;
//! * the first unclassified section after the header is the properties block
//!   (`Name: value` lines plus free-text lines such as flask recovery);
//! * remaining free-text sections are modifiers. Each line may end with a marker
//!   (`(implicit)`, `(rune)`, `(added rune)`, `(enchant)`, `(crafted)`, `(fractured)`,
//!   `(desecrated)`, `(sanctified)`); unmarked lines are explicit. For uniques only the
//!   first unmarked section holds explicits; later ones are flavour text. Usage-hint
//!   sections ("Right click to ...", "Place into ...") are skipped, as are modifier
//!   sections of currency and gems.
//!
//! Advanced copies (Ctrl+Alt+C) add `{ Prefix Modifier "Hale" (Tier: 7) — Life }`
//! headers before the lines they describe and print rolls as `+42(40-49)`. Ranges are
//! stripped from `text` and recorded in [`AdvancedModInfo::ranges`].
use std::sync::LazyLock;

use regex::Regex;

use crate::{
    error::ParseError,
    item::{
        AdvancedModInfo, Affix, ItemClass, ModKind, Modifier, ParsedItem, Property, Rarity, Requirements, StackSize,
        ValueRange,
    },
    matcher::{extract_values, templatize},
};

/// Section separator used in clipboard text.
pub const SEPARATOR: &str = "--------";

const NUM: &str = r"-?\d+(?:\.\d+)?";

static ROLL_RANGE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(&format!(r"({NUM})\(({NUM})-({NUM})\)")).expect("range regex"));
static ADVANCED_HEADER: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"^\{\s*(?P<kind>[^"(—}]*?)\s*(?:"(?P<name>[^"]*)")?\s*(?:\((?:Tier|Rank):\s*(?P<tier>\d+)\))?\s*(?:—\s*(?P<tags>[^}]*?))?\s*\}$"#)
        .expect("advanced header regex")
});
static PAREN_SUFFIX: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\s*\(([a-z ]+)\)").expect("paren regex"));
static BASE_TIER: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\((?:Tier|Level) (\d+)\)\s*$").expect("tier regex"));

/// Lines that start a usage-hint section rather than modifiers.
const HINT_PREFIXES: &[&str] = &[
    "Right click",
    "Right-click",
    "Place into",
    "Place this item",
    "Place an item",
    "Can be used",
    "Shift click",
    "Used automatically",
    "Travel to",
    "Use on",
    "Combine",
    "This is a Support Gem",
    "Can only hold charges",
    "Socket this into",
    "Take this item",
    "Bring this item",
    "Present this item",
];

/// Parse clipboard text into a [`ParsedItem`]. Accepts `\r\n` or `\n`.
pub fn parse_item(text: &str) -> Result<ParsedItem, ParseError> {
    if text.trim().is_empty() {
        return Err(ParseError::Empty);
    }
    let sections = split_sections(text);
    let mut sections = sections.into_iter();
    let header = sections.next().ok_or(ParseError::Empty)?;
    let mut item = parse_header(&header, text)?;
    let mut state = State::default();
    for section in sections {
        parse_section(&mut item, &mut state, &section);
    }
    finish(&mut item);
    Ok(item)
}

/// Cheap check used by the clipboard poller to decide whether text looks like an item.
pub fn looks_like_item(text: &str) -> bool {
    let t = text.trim_start();
    t.starts_with("Item Class:") || t.starts_with("Rarity: Gem")
}

/// Sections as borrowed, trimmed, non-empty lines (no per-line allocation).
fn split_sections(text: &str) -> Vec<Vec<&str>> {
    let mut out: Vec<Vec<&str>> = vec![Vec::new()];
    for line in text.lines() {
        let line = line.trim();
        if line.len() >= SEPARATOR.len() && line.bytes().all(|b| b == b'-') {
            out.push(Vec::new());
        } else if !line.is_empty() {
            out.last_mut().expect("non-empty").push(line);
        }
    }
    out.retain(|s| !s.is_empty());
    out
}

fn strip_key<'a>(line: &'a str, key: &str) -> Option<&'a str> {
    line.strip_prefix(key).and_then(|r| r.strip_prefix(':')).map(str::trim)
}

fn parse_header(lines: &[&str], raw: &str) -> Result<ParsedItem, ParseError> {
    let mut it = lines.iter().copied().peekable();
    let class = match it.peek().and_then(|l| strip_key(l, "Item Class")) {
        Some(v) => {
            it.next();
            Some(ItemClass::from_header(v))
        }
        None => None,
    };
    let rarity_line = it.next().ok_or(ParseError::NotAnItem)?;
    let Some(rarity_value) = strip_key(rarity_line, "Rarity") else {
        return Err(ParseError::NotAnItem);
    };
    let rarity = Rarity::from_header(rarity_value).ok_or_else(|| ParseError::UnknownRarity(rarity_value.to_owned()))?;
    let class = match class {
        Some(c) => c,
        // Meta gems are the only items copied without an `Item Class:` line.
        None if rarity == Rarity::Gem => ItemClass::Other("Meta Gems".into()),
        None => return Err(ParseError::NotAnItem),
    };
    let names: Vec<&str> = it.collect();
    let (name, base_type) = match (rarity, names.as_slice()) {
        (_, []) => {
            return Err(ParseError::Malformed { section: "header".into(), reason: "missing item name".into() });
        }
        (Rarity::Rare | Rarity::Unique, [name, base, ..]) => (Some((*name).to_owned()), (*base).to_owned()),
        (_, [base, ..]) => (None, (*base).to_owned()),
    };
    let base_type = if rarity == Rarity::Normal {
        base_type.strip_prefix("Superior ").map(str::to_owned).unwrap_or(base_type)
    } else {
        base_type
    };
    let class = refine_class(class, &base_type);
    let category = class.category(rarity);
    let mut item = ParsedItem {
        class,
        category,
        rarity,
        name,
        base_type,
        item_level: None,
        quality: None,
        stack_size: None,
        gem_level: None,
        waystone_tier: None,
        area_level: None,
        sockets: 0,
        requirements: Requirements::default(),
        stats: crate::item::ItemStats::default(),
        properties: Vec::new(),
        modifiers: Vec::new(),
        corrupted: false,
        unidentified: false,
        mirrored: false,
        sanctified: false,
        note: None,
        advanced: false,
        raw: raw.to_owned(),
    };
    if let Some(n) = BASE_TIER.captures(&item.base_type).and_then(|c| c[1].parse().ok()) {
        match item.class {
            ItemClass::Waystone => item.waystone_tier = Some(n),
            ItemClass::UncutGem => item.gem_level = Some(n),
            _ => {}
        }
    }
    Ok(item)
}

/// `Augment` and `Stackable Currency` cover several kinds; refine by base name.
fn refine_class(class: ItemClass, base: &str) -> ItemClass {
    match class {
        ItemClass::Rune if base.contains("Soul Core") || base.starts_with("Soul of ") => ItemClass::SoulCore,
        ItemClass::StackableCurrency => {
            if base.contains("Essence of") {
                ItemClass::Essence
            } else if base.starts_with("Distilled ") {
                ItemClass::DistilledEmotion
            } else if base.ends_with("Splinter") {
                ItemClass::Splinter
            } else if base.ends_with("Catalyst") {
                ItemClass::Catalyst
            } else if base.starts_with("Omen of") {
                ItemClass::Omen
            } else {
                class
            }
        }
        other => other,
    }
}

#[derive(Debug, Default)]
struct State {
    properties_done: bool,
    explicits_done: bool,
    advanced: Option<(AdvancedModInfo, ModKind)>,
}

const FLAGS: &[&str] = &[
    "Corrupted",
    "Twice Corrupted",
    "Unidentified",
    "Mirrored",
    "Sanctified",
    "Fractured Item",
    "Split",
    "Unmodifiable",
];

fn is_flag(line: &str) -> bool {
    FLAGS.contains(&line) || line.starts_with("Unidentified (")
}

fn parse_section(item: &mut ParsedItem, state: &mut State, lines: &[&str]) {
    let first = lines[0];
    if lines.iter().all(|l| is_flag(l)) {
        for l in lines {
            match *l {
                "Corrupted" | "Twice Corrupted" => item.corrupted = true,
                "Mirrored" => item.mirrored = true,
                "Sanctified" => item.sanctified = true,
                l if l.starts_with("Unidentified") => item.unidentified = true,
                _ => {}
            }
        }
        return;
    }
    if let Some(note) = strip_key(first, "Note") {
        item.note = Some(note.to_owned());
        return;
    }
    if first.starts_with("Requires") || first.starts_with("Requirements") {
        for l in lines {
            parse_requirement_line(&mut item.requirements, l);
        }
        state.properties_done = true;
        return;
    }
    if ["Sockets", "Item Level", "Stack Size", "Area Level"].iter().any(|k| strip_key(first, k).is_some()) {
        for l in lines {
            parse_property_line(item, l);
        }
        state.properties_done = true;
        return;
    }
    if !state.properties_done {
        state.properties_done = true;
        if !lines.iter().any(|l| l.starts_with('{')) {
            for l in lines {
                parse_property_line(item, l);
            }
            return;
        }
    }
    parse_mod_section(item, state, lines);
}

fn parse_requirement_line(req: &mut Requirements, line: &str) {
    let clean = strip_paren_suffixes(line);
    let body = clean.strip_prefix("Requires:").or_else(|| clean.strip_prefix("Requires"));
    let set = |req: &mut Requirements, key: &str, n: u32| match key.to_ascii_lowercase().as_str() {
        "level" => req.level = Some(n),
        "str" | "strength" => req.str = Some(n),
        "dex" | "dexterity" => req.dex = Some(n),
        "int" | "intelligence" => req.int = Some(n),
        _ => {}
    };
    if let Some(body) = body {
        for token in body.split(',') {
            let words: Vec<&str> = token.split_whitespace().collect();
            match words.as_slice() {
                [a, b] => {
                    if let Ok(n) = a.parse() {
                        set(req, b, n);
                    } else if let Ok(n) = b.parse() {
                        set(req, a, n);
                    }
                }
                [a] => {
                    if let Ok(n) = a.parse() {
                        req.level = Some(n);
                    }
                }
                _ => {}
            }
        }
    } else if let Some((k, v)) = clean.split_once(':')
        && let Ok(n) = v.trim().parse()
    {
        set(req, k.trim(), n);
    }
}

/// Remove `(augmented)`-style suffixes; lines without `(` skip the regex entirely.
fn strip_paren_suffixes(s: &str) -> std::borrow::Cow<'_, str> {
    if s.contains('(') { PAREN_SUFFIX.replace_all(s, "") } else { std::borrow::Cow::Borrowed(s) }
}

fn first_number<T: std::str::FromStr>(s: &str) -> Option<T> {
    crate::matcher::NUMBER.find(s).and_then(|m| m.as_str().replace(',', "").parse().ok())
}

/// Sum of the averages of every `a-b` damage range in `value`.
fn damage_average(value: &str) -> f64 {
    let nums = extract_values(value);
    nums.chunks(2).map(|c| c.iter().sum::<f64>() / c.len() as f64).sum()
}

fn parse_property_line(item: &mut ParsedItem, line: &str) {
    let Some((key, value)) = line.split_once(": ") else {
        item.properties.push(Property { name: line.to_owned(), value: String::new(), augmented: false });
        return;
    };
    let augmented = value.contains("(augmented)");
    let clean = strip_paren_suffixes(value).trim().to_owned();
    match key {
        "Item Level" => {
            item.item_level = first_number(&clean);
            return;
        }
        "Sockets" => {
            item.sockets = clean.split_whitespace().count().min(u8::MAX as usize) as u8;
            return;
        }
        "Stack Size" => {
            let mut parts = clean.split('/').map(first_number::<u32>);
            if let (Some(Some(current)), Some(Some(max))) = (parts.next(), parts.next()) {
                item.stack_size = Some(StackSize { current, max });
            }
        }
        "Quality" => item.quality = first_number(&clean),
        "Level" => item.gem_level = first_number(&clean),
        "Waystone Tier" | "Map Tier" => item.waystone_tier = first_number(&clean),
        "Area Level" => item.area_level = first_number(&clean),
        "Armour" => item.stats.armour = first_number(&clean),
        "Evasion Rating" => item.stats.evasion = first_number(&clean),
        "Energy Shield" => item.stats.energy_shield = first_number(&clean),
        "Spirit" => item.stats.spirit = first_number(&clean),
        "Block chance" | "Chance to Block" | "Block Chance" => item.stats.block_chance = first_number(&clean),
        "Critical Hit Chance" | "Critical Strike Chance" => item.stats.critical_hit_chance = first_number(&clean),
        "Attacks per Second" => item.stats.attacks_per_second = first_number(&clean),
        "Physical Damage" => item.stats.physical_dps = Some(damage_average(&clean)),
        "Elemental Damage" | "Fire Damage" | "Cold Damage" | "Lightning Damage" => {
            item.stats.elemental_dps = Some(item.stats.elemental_dps.unwrap_or(0.0) + damage_average(&clean));
        }
        "Chaos Damage" => item.stats.total_dps = Some(item.stats.total_dps.unwrap_or(0.0) + damage_average(&clean)),
        _ => {}
    }
    item.properties.push(Property { name: key.to_owned(), value: clean, augmented });
}

fn marker_kind(marker: &str) -> Option<ModKind> {
    Some(match marker {
        "implicit" => ModKind::Implicit,
        "rune" | "added rune" => ModKind::Rune,
        "enchant" => ModKind::Enchant,
        "crafted" => ModKind::Crafted,
        "fractured" => ModKind::Fractured,
        "desecrated" => ModKind::Desecrated,
        "sanctified" => ModKind::Sanctified,
        _ => return None,
    })
}

fn split_marker(line: &str) -> (&str, Option<ModKind>) {
    if let Some(open) = line.rfind(" (")
        && line.ends_with(')')
        && let Some(kind) = marker_kind(&line[open + 2..line.len() - 1])
    {
        return (line[..open].trim_end(), Some(kind));
    }
    (line, None)
}

fn parse_advanced_header(line: &str) -> Option<(AdvancedModInfo, ModKind)> {
    let caps = ADVANCED_HEADER.captures(line)?;
    let kind_text = caps.name("kind").map_or("", |m| m.as_str());
    let affix = if kind_text.contains("Prefix") {
        Some(Affix::Prefix)
    } else if kind_text.contains("Suffix") {
        Some(Affix::Suffix)
    } else if kind_text.contains("Implicit") {
        Some(Affix::Implicit)
    } else {
        None
    };
    let kind = if kind_text.contains("Implicit") {
        ModKind::Implicit
    } else if kind_text.contains("Fractured") {
        ModKind::Fractured
    } else if kind_text.contains("Desecrated") {
        ModKind::Desecrated
    } else if kind_text.contains("Crafted") {
        ModKind::Crafted
    } else if kind_text.contains("Enhancement") || kind_text.contains("Enchant") {
        ModKind::Enchant
    } else if kind_text.contains("Rune") || kind_text.contains("Augment") {
        ModKind::Rune
    } else {
        ModKind::Explicit
    };
    let info = AdvancedModInfo {
        affix,
        name: caps.name("name").map(|m| m.as_str().to_owned()),
        tier: caps.name("tier").and_then(|m| m.as_str().parse().ok()),
        ranges: Vec::new(),
    };
    Some((info, kind))
}

fn parse_mod_section(item: &mut ParsedItem, state: &mut State, lines: &[&str]) {
    let first = lines[0];
    if HINT_PREFIXES.iter().any(|p| first.starts_with(p)) || matches!(item.rarity, Rarity::Currency | Rarity::Gem) {
        return;
    }
    let advanced = lines.iter().any(|l| l.starts_with('{'));
    let all_marked = lines.iter().all(|l| split_marker(l).1.is_some() || l.starts_with("Grants Skill:"));
    if !advanced && !all_marked {
        if state.explicits_done && item.rarity == Rarity::Unique {
            return; // flavour text
        }
        state.explicits_done = true;
    }
    if advanced {
        item.advanced = true;
        state.explicits_done = true;
    }
    state.advanced = None;
    for &line in lines {
        if line.starts_with('{') {
            state.advanced = parse_advanced_header(line);
            continue;
        }
        if (line.starts_with('(') && line.ends_with(')'))
            || line == "Desecrated Prefix"
            || line == "Desecrated Suffix"
            || line.starts_with("Unrevealed")
        {
            continue; // reminder text / unrevealed placeholders
        }
        item.modifiers.push(parse_mod_line(line, state.advanced.as_ref()));
    }
}

fn parse_mod_line(line: &str, advanced: Option<&(AdvancedModInfo, ModKind)>) -> Modifier {
    let line = line.strip_suffix("— Unscalable Value").map_or(line, str::trim_end);
    let (body, marker) = split_marker(line);
    let mut ranges = Vec::new();
    // Rolls (`+42(40-49)`) only appear in advanced copies; skip the regex pass otherwise.
    let text = if body.contains('(') {
        ROLL_RANGE
            .replace_all(body, |c: &regex::Captures<'_>| {
                let a: f64 = c[2].parse().unwrap_or_default();
                let b: f64 = c[3].parse().unwrap_or_default();
                ranges.push(ValueRange { min: a.min(b), max: a.max(b) });
                c[1].to_owned()
            })
            .into_owned()
    } else {
        body.to_owned()
    };
    let kind = if body.starts_with("Grants Skill:") {
        ModKind::Skill
    } else {
        marker.or(advanced.map(|(_, k)| *k)).unwrap_or(ModKind::Explicit)
    };
    let advanced = advanced.map(|(info, _)| AdvancedModInfo { ranges, ..info.clone() });
    Modifier { template: templatize(&text), values: extract_values(&text), text, kind, stat_id: None, advanced }
}

fn finish(item: &mut ParsedItem) {
    let s = &mut item.stats;
    if let Some(aps) = s.attacks_per_second {
        let round = |v: f64| (v * 100.0).round() / 100.0;
        let phys = s.physical_dps.map(|avg| round(avg * aps));
        let ele = s.elemental_dps.map(|avg| round(avg * aps));
        let chaos = s.total_dps.map(|avg| round(avg * aps));
        s.physical_dps = phys;
        s.elemental_dps = ele;
        s.total_dps = match (phys, ele, chaos) {
            (None, None, None) => None,
            (p, e, c) => Some(round(p.unwrap_or(0.0) + e.unwrap_or(0.0) + c.unwrap_or(0.0))),
        };
    } else {
        s.physical_dps = None;
        s.elemental_dps = None;
        s.total_dps = None;
    }
}

#[cfg(test)]
mod tests {
    use pretty_assertions::assert_eq;
    use proptest::prelude::*;

    use super::*;
    use crate::item::ItemCategory;

    #[test]
    fn smoke_looks_like_item() {
        assert!(looks_like_item("Item Class: Stackable Currency\nRarity: Currency\nDivine Orb"));
        assert!(looks_like_item("Rarity: Gem\nCoil of Fire"));
        assert!(!looks_like_item("hello"));
    }

    #[test]
    fn errors() {
        assert_eq!(parse_item("  \n"), Err(ParseError::Empty));
        assert_eq!(parse_item("hello world"), Err(ParseError::NotAnItem));
        assert_eq!(parse_item("Item Class: Rings"), Err(ParseError::NotAnItem));
        assert_eq!(parse_item("Item Class: Rings\nBogus"), Err(ParseError::NotAnItem));
        assert_eq!(parse_item("Rarity: Rare\nFoo"), Err(ParseError::NotAnItem));
        assert_eq!(
            parse_item("Item Class: Rings\nRarity: Legendary\nX"),
            Err(ParseError::UnknownRarity("Legendary".into()))
        );
        assert!(matches!(parse_item("Item Class: Rings\nRarity: Rare"), Err(ParseError::Malformed { .. })));
        assert_eq!(parse_item("--------\n--------"), Err(ParseError::Empty));
    }

    const RARE_HELM: &str = "Item Class: Helmets
Rarity: Rare
Doom Visor
Superior Expert Hunter Hood
--------
Quality: +20% (augmented)
Evasion Rating: 312 (augmented)
--------
Requires: Level 65, 87 (unmet) Dex
--------
Sockets: S S
--------
Item Level: 79
--------
+12% to Fire Resistance (rune)
--------
+42 to maximum Life
+31% to Cold Resistance
Totally made-up line (fractured)
--------
Corrupted
Mirrored
--------
Note: ~price 2 divine
";

    #[test]
    fn rare_helmet() {
        let item = parse_item(RARE_HELM).unwrap();
        assert_eq!(item.class, ItemClass::Helmet);
        assert_eq!(item.category, ItemCategory::Equipment);
        assert_eq!(item.name.as_deref(), Some("Doom Visor"));
        assert_eq!(item.base_type, "Superior Expert Hunter Hood");
        assert_eq!(item.quality, Some(20));
        assert_eq!(item.stats.evasion, Some(312));
        assert_eq!(item.requirements, Requirements { level: Some(65), dex: Some(87), ..Default::default() });
        assert_eq!(item.sockets, 2);
        assert_eq!(item.item_level, Some(79));
        assert_eq!(item.modifiers.len(), 4);
        assert_eq!(item.modifiers[0].kind, ModKind::Rune);
        assert_eq!(item.modifiers[0].text, "+12% to Fire Resistance");
        assert_eq!(item.modifiers[1].template, "+# to maximum Life");
        assert_eq!(item.modifiers[1].values, vec![42.0]);
        assert_eq!(item.modifiers[3].kind, ModKind::Fractured);
        assert!(item.corrupted && item.mirrored && !item.unidentified);
        assert_eq!(item.note.as_deref(), Some("~price 2 divine"));
        assert!(item.properties[0].augmented);
        assert!(!item.advanced);
    }

    #[test]
    fn crlf_matches_lf() {
        let crlf = RARE_HELM.replace('\n', "\r\n");
        let mut a = parse_item(RARE_HELM).unwrap();
        let mut b = parse_item(&crlf).unwrap();
        a.raw.clear();
        b.raw.clear();
        assert_eq!(a, b);
    }

    #[test]
    fn multi_line_requirements_and_superior() {
        let text = "Item Class: Gloves\nRarity: Normal\nSuperior Stocky Mitts\n--------\nRequirements:\nLevel: 12\nStr: 20\nDexterity: 5\nInt: 7\nBogus\n--------\nItem Level: 15";
        let item = parse_item(text).unwrap();
        assert_eq!(item.base_type, "Stocky Mitts");
        assert_eq!(item.requirements, Requirements { level: Some(12), str: Some(20), dex: Some(5), int: Some(7) });
        let mut req = Requirements::default();
        parse_requirement_line(&mut req, "Requires: Str 30, 40, Strange Thing Here, 50 Intelligence");
        assert_eq!(req, Requirements { level: Some(40), str: Some(30), int: Some(50), dex: None });
    }

    #[test]
    fn weapon_dps() {
        let text = "Item Class: Bows\nRarity: Rare\nHate Thirst\nWarmonger Bow\n--------\nPhysical Damage: 50-100 (augmented)\nElemental Damage: 10-20 (fire), 5-15 (cold)\nLightning Damage: 1-9 (lightning)\nChaos Damage: 2-4\nCritical Hit Chance: 5.00%\nAttacks per Second: 1.20\n--------\nItem Level: 80\n--------\nAdds 1 to 4 Lightning Damage";
        let item = parse_item(text).unwrap();
        assert_eq!(item.stats.physical_dps, Some(90.0));
        assert_eq!(item.stats.elemental_dps, Some(36.0));
        assert_eq!(item.stats.total_dps, Some(129.6));
        assert_eq!(item.stats.critical_hit_chance, Some(5.0));
        // Without APS no DPS is reported.
        let no_aps =
            parse_item("Item Class: Bows\nRarity: Normal\nShort Bow\n--------\nPhysical Damage: 5-10").unwrap();
        assert_eq!(no_aps.stats.physical_dps, None);
    }

    #[test]
    fn advanced_copy() {
        let text = "Item Class: Rings\nRarity: Rare\nRapture Loop\nRuby Ring\n--------\nItem Level: 81\n--------\n{ Implicit Modifier — Elemental, Fire, Resistance }\n+25(20-30)% to Fire Resistance\n--------\n{ Prefix Modifier \"Hale\" (Tier: 7) — Life }\n+42(40-49) to maximum Life\n(Reminder text here)\n{ Suffix Modifier \"of Infamy\" — Attack, Speed }\n14(15-10)% reduced Attack Speed — Unscalable Value\n{ Fractured Prefix Modifier \"Electrocuting\" (Rank: 2) — Lightning }\nAdds 1(1-4) to 50(46-66) Lightning Damage to Attacks\n{ Desecrated Suffix Modifier \"of Amanamu\" (Tier: 1) }\n+10% to Chaos Resistance\n{ Crafted Suffix Modifier }\n+5 to Strength\n{ Corruption Enhancement }\n+1 to Level of all Skills\n{ Unique Modifier — Defences }\n+3 to Spirit\n{ Rune Modifier }\n+2 to Dexterity\nDesecrated Prefix\n";
        let item = parse_item(text).unwrap();
        assert!(item.advanced);
        let m = &item.modifiers;
        assert_eq!(m.len(), 9);
        assert_eq!(m[0].kind, ModKind::Implicit);
        assert_eq!(m[0].text, "+25% to Fire Resistance");
        let adv = m[0].advanced.as_ref().unwrap();
        assert_eq!(adv.affix, Some(Affix::Implicit));
        assert_eq!(adv.ranges, vec![ValueRange { min: 20.0, max: 30.0 }]);
        let hale = m[1].advanced.as_ref().unwrap();
        assert_eq!((hale.affix, hale.name.as_deref(), hale.tier), (Some(Affix::Prefix), Some("Hale"), Some(7)));
        assert_eq!(m[2].text, "14% reduced Attack Speed");
        assert_eq!(m[2].advanced.as_ref().unwrap().ranges, vec![ValueRange { min: 10.0, max: 15.0 }]);
        assert_eq!(m[2].advanced.as_ref().unwrap().affix, Some(Affix::Suffix));
        assert_eq!(m[3].kind, ModKind::Fractured);
        assert_eq!(m[3].advanced.as_ref().unwrap().ranges.len(), 2);
        assert_eq!(m[3].advanced.as_ref().unwrap().tier, Some(2));
        assert_eq!(m[4].kind, ModKind::Desecrated);
        assert_eq!(m[5].kind, ModKind::Crafted);
        assert_eq!(m[6].kind, ModKind::Enchant);
        assert_eq!(m[7].kind, ModKind::Explicit);
        assert_eq!(m[7].advanced.as_ref().unwrap().affix, None);
        assert_eq!(m[8].kind, ModKind::Rune);
    }

    #[test]
    fn unique_flavour_and_skills() {
        let text = "Item Class: Sceptres\nRarity: Unique\nThe Dark Defiler\nRattling Sceptre\n--------\nSpirit: 100\n--------\nItem Level: 70\n--------\nGrants Skill: Level 12 Skeletal Warrior\n--------\n+20 to Intelligence\n--------\nA dark gift.\n--------\nUnidentified (Tier 4)";
        let item = parse_item(text).unwrap();
        assert_eq!(item.category, ItemCategory::Unique);
        assert_eq!(item.stats.spirit, Some(100));
        assert_eq!(item.modifiers.len(), 2);
        assert_eq!(item.modifiers[0].kind, ModKind::Skill);
        assert!(item.unidentified);
    }

    #[test]
    fn meta_gem_and_refinements() {
        let gem = parse_item("Rarity: Gem\nCast on Critical\n--------\nLevel: 5\n--------\nSome text").unwrap();
        assert_eq!(gem.class, ItemClass::Other("Meta Gems".into()));
        assert_eq!(gem.category, ItemCategory::Gem);
        assert_eq!(gem.gem_level, Some(5));
        let uncut = parse_item("Item Class: Uncut Skill Gems\nRarity: Currency\nUncut Skill Gem (Level 19)").unwrap();
        assert_eq!(uncut.gem_level, Some(19));
        let way = parse_item("Item Class: Waystones\nRarity: Normal\nWaystone (Tier 15)").unwrap();
        assert_eq!(way.waystone_tier, Some(15));
        let core = parse_item("Item Class: Augment\nRarity: Currency\nSoul Core of Tacati").unwrap();
        assert_eq!(core.class, ItemClass::SoulCore);
        for (base, class) in [
            ("Greater Essence of the Body", ItemClass::Essence),
            ("Distilled Ire", ItemClass::DistilledEmotion),
            ("Breach Splinter", ItemClass::Splinter),
            ("Flesh Catalyst", ItemClass::Catalyst),
            ("Omen of Amelioration", ItemClass::Omen),
            ("Exalted Orb", ItemClass::StackableCurrency),
        ] {
            let item = parse_item(&format!("Item Class: Stackable Currency\nRarity: Currency\n{base}")).unwrap();
            assert_eq!(item.class, class, "{base}");
        }
    }

    #[test]
    fn stack_size_and_area_level() {
        let item = parse_item("Item Class: Stackable Currency\nRarity: Currency\nExalted Orb\n--------\nStack Size: 1,234/5,000\nArea Level: 70").unwrap();
        assert_eq!(item.stack_size, Some(StackSize { current: 1234, max: 5000 }));
        assert_eq!(item.area_level, Some(70));
    }

    proptest! {
        #[test]
        fn never_panics(s in "\\PC{0,400}") {
            let _ = parse_item(&s);
        }

        #[test]
        fn never_panics_structured(
            lines in proptest::collection::vec(
                prop_oneof![
                    Just("--------".to_owned()),
                    Just("Item Class: Helmets".to_owned()),
                    Just("Rarity: Rare".to_owned()),
                    Just("{ Prefix Modifier \"X\" (Tier: 1) — Y }".to_owned()),
                    Just("Requires: Level".to_owned()),
                    Just("Sockets:".to_owned()),
                    "[ -~]{0,40}",
                    "[0-9()+%:, -]{0,20}",
                ],
                0..30,
            )
        ) {
            let _ = parse_item(&lines.join("\n"));
        }
    }
}
