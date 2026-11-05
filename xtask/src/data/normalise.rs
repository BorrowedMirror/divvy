//! Pure normalisation from upstream JSON into the `rkl_core::catalog` model.
//!
//! Every function here is deterministic: outputs are sorted and de-duplicated so that
//! re-running the refresh on identical inputs produces byte-identical assets.
use std::collections::{BTreeMap, BTreeSet, HashMap};

use anyhow::Context as _;
use rkl_core::catalog::{BaseDef, StatAlias, StatDef, StaticDef, UniqueDef};
use serde::Deserialize;

/// Host serving the relative `image` paths of `/api/trade2/data/static`.
pub const IMAGE_HOST: &str = "https://web.poecdn.com";

/// `{"result": [group, ...]}` envelope shared by the trade2 data endpoints.
#[derive(Debug, Deserialize)]
struct Envelope<G> {
    result: Vec<G>,
}

#[derive(Debug, Deserialize)]
struct StatGroup {
    id: String,
    entries: Vec<StatEntry>,
}

#[derive(Debug, Deserialize)]
struct StatEntry {
    id: String,
    text: String,
}

#[derive(Debug, Deserialize)]
struct ItemGroup {
    id: String,
    label: String,
    entries: Vec<ItemEntry>,
}

#[derive(Debug, Deserialize)]
struct ItemEntry {
    #[serde(rename = "type")]
    base_type: String,
    name: Option<String>,
    disc: Option<String>,
    #[serde(default)]
    flags: ItemFlags,
}

#[derive(Debug, Default, Deserialize)]
struct ItemFlags {
    #[serde(default)]
    unique: bool,
}

#[derive(Debug, Deserialize)]
struct StaticGroup {
    id: String,
    entries: Vec<StaticEntry>,
}

#[derive(Debug, Deserialize)]
struct StaticEntry {
    id: String,
    text: String,
    image: Option<String>,
}

/// RePoE `base_items.json` value (keyed by metadata path).
#[derive(Debug, Deserialize)]
pub struct RepoeBase {
    pub name: String,
    pub item_class: String,
    #[serde(default)]
    pub release_state: String,
}

/// RePoE `item_classes.json` value (keyed by item class id).
#[derive(Debug, Deserialize)]
pub struct RepoeItemClass {
    #[serde(default)]
    pub name: String,
}

/// Alternative wordings for a trade stat template.
///
/// * sign variants: `# to X` also matches `+# to X` and (negated) `-# to X`; `+# to X`
///   also matches `# to X` and (negated) `-# to X`.
/// * `#% increased` <-> `#% reduced` and `#% more` <-> `#% less` (negated).
///
/// Values of a negated alias must be multiplied by -1 (the matcher reads unsigned
/// numbers from the templatized line).
pub fn stat_aliases(text: &str) -> Vec<StatAlias> {
    let mut out: Vec<StatAlias> = Vec::new();
    let mut push = |text: String, negate: bool| {
        if !out.iter().any(|a| a.text == text) {
            out.push(StatAlias { text, negate });
        }
    };
    if let Some(rest) = text.strip_prefix("+#") {
        push(format!("#{rest}"), false);
        push(format!("-#{rest}"), true);
    } else if text.starts_with('#') {
        push(format!("+{text}"), false);
        push(format!("-{text}"), true);
    }
    for (from, to) in
        [("% increased ", "% reduced "), ("% reduced ", "% increased "), ("% more ", "% less "), ("% less ", "% more ")]
    {
        if text.contains(from) {
            push(text.replacen(from, to, 1), true);
            break;
        }
    }
    out.retain(|a| a.text != text);
    out
}

/// Exact alternative wordings of a trade template as it appears on items (never negated):
///
/// * a trailing disambiguation suffix is dropped: `#% increased Armour (Local)` ->
///   `#% increased Armour`, `#% increased Duration (Charm)` -> `#% increased Duration`
///   (items never print these suffixes; the matcher disambiguates local/global by item);
/// * map-device wording: `... in Map` -> `... in your Maps` (tablets print the latter);
/// * literal numbers baked into the template are templatized the way the matcher does
///   (`... per 10 Dexterity` -> `... per # Dexterity`, `for 60 seconds` -> `for # seconds`);
/// * level-less skill grants: `Grants Skill: Level # Parry` -> `Grants Skill: Parry`.
pub fn wording_variants(text: &str) -> Vec<String> {
    let mut out: Vec<String> = vec![text.to_owned()];
    let push = |out: &mut Vec<String>, t: String| {
        if !out.contains(&t) {
            out.push(t);
        }
    };
    if let Some(stripped) = strip_suffix(text) {
        push(&mut out, stripped.to_owned());
    }
    for i in 0..out.len() {
        let maps = in_your_maps(&out[i]);
        if let Some(maps) = maps {
            push(&mut out, maps);
        }
    }
    for i in 0..out.len() {
        let skill = out[i].strip_prefix("Grants Skill: Level # ").map(|s| format!("Grants Skill: {s}"));
        let templated = rkl_core::matcher::templatize(&out[i]);
        push(&mut out, templated);
        if let Some(skill) = skill {
            push(&mut out, skill);
        }
    }
    out.remove(0);
    out
}

/// `X (Local)` -> `X`, for a trailing parenthesised capitalised label.
fn strip_suffix(text: &str) -> Option<&str> {
    let body = text.strip_suffix(')')?;
    let open = body.rfind(" (")?;
    let label = &body[open + 2..];
    let is_label = label.starts_with(|c: char| c.is_ascii_uppercase())
        && label.chars().all(|c| c.is_ascii_alphabetic() || c == ' ');
    is_label.then(|| &text[..open])
}

/// Replace the whole word `in Map` with `in your Maps`.
fn in_your_maps(text: &str) -> Option<String> {
    let at = text.match_indices(" in Map").find(|(i, m)| {
        let after = &text[i + m.len()..];
        after.is_empty() || after.starts_with(' ')
    })?;
    Some(format!("{} in your Maps{}", &text[..at.0], &text[at.0 + at.1.len()..]))
}

/// `/api/trade2/data/stats` -> stats sorted by `(kind, id)`, with aliases.
///
/// `kind` is the trade2 group id (`explicit`, `implicit`, `rune`, ...). trade2 lists some
/// ids several times with different wordings (`#% increased Experience gain` and
/// `#% increased Experience gain in Map`); the first is `text`, the others become
/// non-negated aliases, as do [`wording_variants`]. Generated [`stat_aliases`] that collide
/// with a real template of the same kind are dropped so they cannot create false
/// ambiguities; real wordings and wording variants are always kept.
pub fn stats(json: &str) -> anyhow::Result<Vec<StatDef>> {
    let env: Envelope<StatGroup> = serde_json::from_str(json).context("parsing trade2 stats")?;
    let mut texts: BTreeMap<(String, String), Vec<String>> = BTreeMap::new();
    for group in env.result {
        for e in group.entries {
            let t = texts.entry((group.id.clone(), e.id)).or_default();
            if !t.contains(&e.text) {
                t.push(e.text);
            }
        }
    }
    let real: BTreeSet<(&str, &str)> =
        texts.iter().flat_map(|((kind, _), ts)| ts.iter().map(move |t| (kind.as_str(), t.as_str()))).collect();
    let mut out = Vec::with_capacity(texts.len());
    for ((kind, id), ts) in &texts {
        let primary = &ts[0];
        let mut wordings: Vec<String> = Vec::new();
        for t in ts {
            if t != primary {
                wordings.push(t.clone());
            }
            wordings.extend(wording_variants(t));
        }
        let mut aliases: Vec<StatAlias> = Vec::new();
        let mut push = |alias: StatAlias| {
            if alias.text != *primary && !aliases.iter().any(|a| a.text == alias.text) {
                aliases.push(alias);
            }
        };
        for w in &wordings {
            push(StatAlias { text: w.clone(), negate: false });
        }
        for w in std::iter::once(primary).chain(&wordings) {
            for a in stat_aliases(w) {
                if !real.contains(&(kind.as_str(), a.text.as_str())) {
                    push(a);
                }
            }
        }
        out.push(StatDef { id: id.clone(), text: primary.clone(), kind: kind.clone(), aliases });
    }
    Ok(out)
}

/// Trade category id (`/api/trade2/data/filters` `type_filters.category`) for a RePoE
/// item class id.
pub fn trade_category(item_class: &str) -> Option<&'static str> {
    Some(match item_class {
        "Helmet" => "armour.helmet",
        "Body Armour" => "armour.chest",
        "Gloves" => "armour.gloves",
        "Boots" => "armour.boots",
        "Shield" => "armour.shield",
        "Buckler" => "armour.buckler",
        "Focus" => "armour.focus",
        "Quiver" => "armour.quiver",
        "Ring" => "accessory.ring",
        "Amulet" => "accessory.amulet",
        "Belt" => "accessory.belt",
        "Jewel" => "jewel",
        "LifeFlask" => "flask.life",
        "ManaFlask" => "flask.mana",
        "UtilityFlask" => "flask.charm",
        "Claw" => "weapon.claw",
        "Dagger" => "weapon.dagger",
        "One Hand Sword" => "weapon.onesword",
        "One Hand Axe" => "weapon.oneaxe",
        "One Hand Mace" => "weapon.onemace",
        "Spear" => "weapon.spear",
        "Flail" => "weapon.flail",
        "Two Hand Sword" => "weapon.twosword",
        "Two Hand Axe" => "weapon.twoaxe",
        "Two Hand Mace" => "weapon.twomace",
        "Warstaff" => "weapon.warstaff",
        "Talisman" => "weapon.talisman",
        "Bow" => "weapon.bow",
        "Crossbow" => "weapon.crossbow",
        "Wand" => "weapon.wand",
        "Sceptre" => "weapon.sceptre",
        "Staff" => "weapon.staff",
        "Map" => "map.waystone",
        "Breachstone" => "map.breachstone",
        "TowerAugmentation" => "map.tablet",
        "UltimatumKey" => "map.ultimatum",
        "Relic" => "sanctum.relic",
        "Omen" => "currency.omen",
        "SoulCore" => "currency.soulcore",
        "Active Skill Gem" => "gem.activegem",
        "Support Skill Gem" => "gem.supportgem",
        "Meta Skill Gem" => "gem.metagem",
        _ => return None,
    })
}

/// `/api/trade2/data/items` (non-unique entries) enriched with RePoE item classes ->
/// bases sorted by name.
///
/// `category` comes from the RePoE item class when known, else the trade2 group id;
/// `item_class` is the in-game `Item Class:` header (RePoE item class display name), else
/// the trade2 group label.
pub fn bases(
    items_json: &str,
    repoe_bases: &HashMap<String, RepoeBase>,
    repoe_classes: &HashMap<String, RepoeItemClass>,
) -> anyhow::Result<Vec<BaseDef>> {
    let env: Envelope<ItemGroup> = serde_json::from_str(items_json).context("parsing trade2 items")?;
    // name -> item class id, preferring released bases with a known trade category and
    // then the lexicographically smallest metadata path (HashMap order is random).
    let mut class_of: BTreeMap<&str, (bool, bool, &str, &str)> = BTreeMap::new();
    for (path, b) in repoe_bases {
        let rank = (b.release_state != "released", trade_category(&b.item_class).is_none(), path.as_str());
        let entry = class_of.entry(b.name.as_str()).or_insert((rank.0, rank.1, rank.2, &b.item_class));
        if rank < (entry.0, entry.1, entry.2) {
            *entry = (rank.0, rank.1, rank.2, &b.item_class);
        }
    }
    let mut out: BTreeMap<String, BaseDef> = BTreeMap::new();
    for group in &env.result {
        for e in group.entries.iter().filter(|e| e.name.is_none() && !e.flags.unique) {
            let class_id = class_of.get(e.base_type.as_str()).map(|c| c.3);
            let category = class_id.and_then(trade_category).unwrap_or(&group.id).to_owned();
            let item_class = class_id
                .and_then(|c| repoe_classes.get(c))
                .map(|c| c.name.clone())
                .filter(|n| !n.is_empty())
                .unwrap_or_else(|| group.label.clone());
            out.entry(e.base_type.clone()).or_insert(BaseDef { name: e.base_type.clone(), category, item_class });
        }
    }
    Ok(out.into_values().collect())
}

/// `/api/trade2/data/items` unique entries -> uniques sorted by `(name, base_type)`.
/// Legacy variants (entries with a `disc`) are skipped.
pub fn uniques(items_json: &str) -> anyhow::Result<Vec<UniqueDef>> {
    let env: Envelope<ItemGroup> = serde_json::from_str(items_json).context("parsing trade2 items")?;
    let set: BTreeSet<(String, String)> = env
        .result
        .into_iter()
        .flat_map(|g| g.entries)
        .filter(|e| e.flags.unique && e.disc.is_none())
        .filter_map(|e| Some((e.name?, e.base_type)))
        .collect();
    Ok(set.into_iter().map(|(name, base_type)| UniqueDef { name, base_type }).collect())
}

/// `/api/trade2/data/static` -> statics sorted by `(group, id)`. Separator entries
/// (`id == "sep"`) are skipped; relative image paths are made absolute.
pub fn statics(json: &str) -> anyhow::Result<Vec<StaticDef>> {
    let env: Envelope<StaticGroup> = serde_json::from_str(json).context("parsing trade2 static")?;
    let mut out: BTreeMap<(String, String), StaticDef> = BTreeMap::new();
    for group in env.result {
        for e in group.entries.into_iter().filter(|e| e.id != "sep" && !e.text.is_empty()) {
            let image = e.image.map(|i| if i.starts_with('/') { format!("{IMAGE_HOST}{i}") } else { i });
            out.entry((group.id.clone(), e.id.clone())).or_insert(StaticDef {
                id: e.id,
                text: e.text,
                group: group.id.clone(),
                image,
            });
        }
    }
    Ok(out.into_values().collect())
}

/// Game version from the RePoE index page title (`RePoE - PoE2 version 0.5.5.5`).
pub fn game_version(index_html: &str) -> Option<String> {
    let rest = &index_html[index_html.find("PoE2 version ")? + "PoE2 version ".len()..];
    let version: String = rest.chars().take_while(|c| c.is_ascii_digit() || *c == '.').collect();
    let version = version.trim_end_matches('.');
    (!version.is_empty()).then(|| version.to_owned())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn alias(text: &str, negate: bool) -> StatAlias {
        StatAlias { text: text.to_owned(), negate }
    }

    #[test]
    fn aliases_unsigned_flat() {
        assert_eq!(
            stat_aliases("# to maximum Life"),
            vec![alias("+# to maximum Life", false), alias("-# to maximum Life", true)]
        );
    }

    #[test]
    fn aliases_signed_flat() {
        assert_eq!(
            stat_aliases("+#% to Fire Resistance"),
            vec![alias("#% to Fire Resistance", false), alias("-#% to Fire Resistance", true)]
        );
    }

    #[test]
    fn aliases_increased_reduced_more_less() {
        assert_eq!(
            stat_aliases("#% increased Movement Speed"),
            vec![
                alias("+#% increased Movement Speed", false),
                alias("-#% increased Movement Speed", true),
                alias("#% reduced Movement Speed", true),
            ]
        );
        assert_eq!(
            stat_aliases("Players gain #% reduced Flask Charges"),
            vec![alias("Players gain #% increased Flask Charges", true)]
        );
        assert_eq!(stat_aliases("Deal #% more Damage"), vec![alias("Deal #% less Damage", true)]);
        assert_eq!(stat_aliases("Take #% less Damage"), vec![alias("Take #% more Damage", true)]);
    }

    #[test]
    fn aliases_none_for_plain_text() {
        assert!(stat_aliases("Dazes on Hit").is_empty());
    }

    const STATS: &str = r##"{"result":[
        {"id":"explicit","label":"Explicit","entries":[
            {"id":"explicit.stat_2","text":"#% reduced Mana Cost of Skills","type":"explicit"},
            {"id":"explicit.stat_1","text":"#% increased Mana Cost of Skills","type":"explicit"},
            {"id":"explicit.stat_1","text":"#% increased Mana Cost of Skills","type":"explicit"}
        ]},
        {"id":"implicit","label":"Implicit","entries":[
            {"id":"implicit.stat_3","text":"# to Spirit","type":"implicit"}
        ]}
    ]}"##;

    #[test]
    fn stats_sorted_deduped_and_colliding_aliases_dropped() {
        let s = stats(STATS).unwrap();
        let ids: Vec<_> = s.iter().map(|s| s.id.as_str()).collect();
        assert_eq!(ids, ["explicit.stat_1", "explicit.stat_2", "implicit.stat_3"]);
        assert_eq!(s[0].kind, "explicit");
        // `#% reduced Mana Cost of Skills` is a real stat, so not an alias of stat_1.
        assert!(s[0].aliases.iter().all(|a| a.text != "#% reduced Mana Cost of Skills"));
        assert_eq!(s[2].aliases.len(), 2);
    }

    #[test]
    fn wording_variants_strip_suffix_and_map_wording() {
        assert_eq!(wording_variants("#% increased Armour (Local)"), ["#% increased Armour"]);
        assert_eq!(wording_variants("#% increased Duration (Charm)"), ["#% increased Duration"]);
        assert_eq!(
            wording_variants("#% increased Gold found in Map (Gold Piles)"),
            [
                "#% increased Gold found in Map",
                "#% increased Gold found in your Maps (Gold Piles)",
                "#% increased Gold found in your Maps",
            ]
        );
        assert_eq!(
            wording_variants("#% increased Quantity of Items found in Map"),
            ["#% increased Quantity of Items found in your Maps"]
        );
        assert_eq!(wording_variants("Breaches in Map have # Monsters"), ["Breaches in your Maps have # Monsters"]);
        // Not a whole word / not a label suffix.
        assert!(wording_variants("Delirium in Maps").is_empty());
        assert!(wording_variants("# to Spirit (per socket)").is_empty());
        assert!(wording_variants("Has ()").is_empty());
        assert!(wording_variants("Dazes on Hit").is_empty());
        assert_eq!(
            wording_variants("#% increased Attack Speed per 10 Dexterity"),
            ["#% increased Attack Speed per # Dexterity"]
        );
        assert_eq!(wording_variants("Grants Skill: Level # Parry"), ["Grants Skill: Parry"]);
    }

    #[test]
    fn stats_keep_same_id_wordings_and_local_variants() {
        let json = r##"{"result":[{"id":"explicit","entries":[
            {"id":"explicit.xp","text":"#% increased Experience gain"},
            {"id":"explicit.xp","text":"#% increased Experience gain in Map"},
            {"id":"explicit.armour_local","text":"#% increased Armour (Local)"},
            {"id":"explicit.armour","text":"#% increased Armour"}
        ]}]}"##;
        let s = stats(json).unwrap();
        let by_id = |id: &str| s.iter().find(|s| s.id == id).unwrap();
        let has =
            |id: &str, text: &str, negate: bool| by_id(id).aliases.iter().any(|a| a.text == text && a.negate == negate);
        assert_eq!(by_id("explicit.xp").text, "#% increased Experience gain");
        assert!(has("explicit.xp", "#% increased Experience gain in Map", false));
        assert!(has("explicit.xp", "#% increased Experience gain in your Maps", false));
        assert!(has("explicit.xp", "#% reduced Experience gain in your Maps", true));
        // Local variant keeps the unsuffixed wording even though a global stat has it.
        assert!(has("explicit.armour_local", "#% increased Armour", false));
        assert!(has("explicit.armour_local", "#% reduced Armour", true));
    }

    #[test]
    fn stats_rejects_garbage() {
        assert!(stats("{}").is_err());
    }

    const ITEMS: &str = r#"{"result":[
        {"id":"armour","label":"Armour","entries":[
            {"type":"Hunter Hood"},
            {"type":"Hunter Hood","text":"Elevore Hunter Hood","name":"Elevore","flags":{"unique":true}},
            {"type":"Furtive Wraps","text":"Hand Furtive Wraps (Legacy)","name":"Hand","disc":"legacy","flags":{"unique":true}},
            {"type":"Mystery Plate"},
            {"type":"Hunter Hood"}
        ]},
        {"id":"accessory","label":"Accessories","entries":[
            {"type":"Gold Ring"},
            {"type":"Gold Ring","text":"Andvarius Gold Ring","name":"Andvarius","flags":{"unique":true}}
        ]}
    ]}"#;

    fn repoe() -> (HashMap<String, RepoeBase>, HashMap<String, RepoeItemClass>) {
        let bases = serde_json::from_str(
            r#"{
            "Metadata/Items/Armours/Helmets/FourHelmetDex5": {"name":"Hunter Hood","item_class":"Helmet","release_state":"released"},
            "Metadata/Items/Hidden/HunterHood": {"name":"Hunter Hood","item_class":"HiddenItem","release_state":"unreleased"},
            "Metadata/Items/Rings/Ring1": {"name":"Gold Ring","item_class":"Ring","release_state":"released"}
        }"#,
        )
        .unwrap();
        let classes =
            serde_json::from_str(r#"{"Helmet":{"name":"Helmets"},"Ring":{"name":"Rings"},"HiddenItem":{}}"#).unwrap();
        (bases, classes)
    }

    #[test]
    fn bases_enriched_from_repoe() {
        let (b, c) = repoe();
        let out = bases(ITEMS, &b, &c).unwrap();
        assert_eq!(
            out,
            vec![
                BaseDef { name: "Gold Ring".into(), category: "accessory.ring".into(), item_class: "Rings".into() },
                BaseDef { name: "Hunter Hood".into(), category: "armour.helmet".into(), item_class: "Helmets".into() },
                BaseDef { name: "Mystery Plate".into(), category: "armour".into(), item_class: "Armour".into() },
            ]
        );
    }

    #[test]
    fn uniques_skip_legacy() {
        let out = uniques(ITEMS).unwrap();
        assert_eq!(
            out,
            vec![
                UniqueDef { name: "Andvarius".into(), base_type: "Gold Ring".into() },
                UniqueDef { name: "Elevore".into(), base_type: "Hunter Hood".into() },
            ]
        );
    }

    #[test]
    fn statics_skip_separators_and_absolutise_images() {
        let json = r#"{"result":[
            {"id":"Currency","label":"Currency","entries":[
                {"id":"divine","text":"Divine Orb","image":"/gen/image/divine.png"},
                {"id":"sep","text":""},
                {"id":"chaos","text":"Chaos Orb","image":"https://example.invalid/chaos.png"}
            ]},
            {"id":"UncutGems","label":"Uncut Gems","entries":[{"id":"uncut-1","text":"Uncut Skill Gem (Level 1)"}]}
        ]}"#;
        let out = statics(json).unwrap();
        assert_eq!(out.len(), 3);
        assert_eq!(out[0].id, "chaos");
        assert_eq!(out[0].image.as_deref(), Some("https://example.invalid/chaos.png"));
        assert_eq!(out[1].image.as_deref(), Some("https://web.poecdn.com/gen/image/divine.png"));
        assert_eq!(out[2].group, "UncutGems");
        assert_eq!(out[2].image, None);
    }

    #[test]
    fn game_version_from_index() {
        assert_eq!(game_version("<title>RePoE - PoE2 version 0.5.5.5</title>").as_deref(), Some("0.5.5.5"));
        assert_eq!(game_version("PoE2 version 1.0."), Some("1.0".into()));
        assert_eq!(game_version("nothing here"), None);
        assert_eq!(game_version("PoE2 version x"), None);
    }

    #[test]
    fn categories() {
        assert_eq!(trade_category("Warstaff"), Some("weapon.warstaff"));
        assert_eq!(trade_category("QuestItem"), None);
    }
}
