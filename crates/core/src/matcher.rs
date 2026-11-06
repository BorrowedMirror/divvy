//! Maps parsed modifiers to trade stat ids.
//!
//! Matching works on templates: every number in a modifier line is replaced by `#`
//! ([`templatize`]) and looked up through [`Catalog::stats_by_template`]. When several
//! stats share a template the matcher prefers the `(Local)` variant on weapons and
//! armour and the global one elsewhere. Crafted / fractured / desecrated / sanctified
//! lines fall back to the explicit stat when the catalog has no dedicated kind.
use std::sync::LazyLock;

use regex::Regex;

use crate::{
    catalog::{Catalog, StatDef},
    item::{ItemClass, ModKind, ParsedItem, Rarity},
};

/// Numbers, optionally with thousands separators and decimals (`1,043`, `4.40`).
pub(crate) static NUMBER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\d{1,3}(?:,\d{3})+(?:\.\d+)?|\d+(?:\.\d+)?").expect("number regex"));

/// Replace every number in `line` with `#` (keeping `+`/`-` signs as text), matching trade templates.
pub fn templatize(line: &str) -> String {
    NUMBER.replace_all(line.trim(), "#").into_owned()
}

/// Numeric values of `line` in order of appearance (unsigned; signs stay in the template).
pub fn extract_values(line: &str) -> Vec<f64> {
    NUMBER.find_iter(line).filter_map(|m| m.as_str().replace(',', "").parse().ok()).collect()
}

/// Outcome per modifier.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MatchOutcome {
    Matched { stat_id: String, negated: bool },
    Ambiguous(Vec<String>),
    Unmatched,
}

#[derive(Debug)]
pub struct StatMatcher<'c> {
    catalog: &'c dyn Catalog,
}

impl<'c> StatMatcher<'c> {
    pub fn new(catalog: &'c dyn Catalog) -> Self {
        Self { catalog }
    }

    pub fn catalog(&self) -> &'c dyn Catalog {
        self.catalog
    }

    /// Fill `stat_id` on every modifier that can be matched; returns per-modifier outcomes.
    ///
    /// Also resolves the base type of magic items (whose single header line carries the
    /// affix names): when the catalog finds a base inside the line, the full line moves
    /// to `name` and `base_type` becomes the bare base. Negated matches (e.g. a
    /// "reduced" line matched to an "increased" stat) flip the sign of the values.
    pub fn match_item(&self, item: &mut ParsedItem) -> Vec<MatchOutcome> {
        if item.rarity == Rarity::Magic
            && item.name.is_none()
            && let Some(base) = self.catalog.base_in_magic_name(&item.base_type)
            && base.name != item.base_type
        {
            item.name = Some(std::mem::replace(&mut item.base_type, base.name.clone()));
        }
        let local = prefers_local(&item.class);
        item.modifiers
            .iter_mut()
            .map(|m| {
                let mut outcome = self.match_one(&m.template, m.kind, local);
                if outcome == MatchOutcome::Unmatched
                    && let Some((plural, index)) = pluralise_additional(&m.template)
                {
                    outcome = self.match_one(&plural, m.kind, local);
                    if outcome != MatchOutcome::Unmatched {
                        // "an additional Arrow" carries an implicit value of 1.
                        m.values.insert(index.min(m.values.len()), 1.0);
                    }
                }
                match &outcome {
                    MatchOutcome::Matched { stat_id, negated } => {
                        m.stat_id = Some(stat_id.clone());
                        if *negated {
                            m.values.iter_mut().for_each(|v| *v = -*v);
                        }
                    }
                    MatchOutcome::Ambiguous(_) | MatchOutcome::Unmatched => m.stat_id = None,
                }
                outcome
            })
            .collect()
    }

    fn match_one(&self, template: &str, kind: ModKind, local: bool) -> MatchOutcome {
        let mut candidates = self.candidates(template, kind, local);
        if candidates.is_empty()
            && let Some(rest) = template.strip_prefix('-')
        {
            // `-#% to Fire Resistance` is listed as `+#% to Fire Resistance` with negative values.
            candidates = self.candidates(&format!("+{rest}"), kind, local).into_iter().map(|(s, n)| (s, !n)).collect();
        }
        match candidates.len() {
            0 => MatchOutcome::Unmatched,
            1 => {
                let (stat, negated) = candidates[0];
                MatchOutcome::Matched { stat_id: stat.id.clone(), negated }
            }
            _ => {
                let (stat, negated) = break_tie(candidates, template, local);
                MatchOutcome::Matched { stat_id: stat.id.clone(), negated }
            }
        }
    }

    /// Candidates for `template`; on weapons/armour the `(Local)` variants win when present.
    fn candidates(&self, template: &str, kind: ModKind, local: bool) -> Vec<(&'c StatDef, bool)> {
        if local {
            let locals = self.lookup(&format!("{template} (Local)"), kind);
            if !locals.is_empty() {
                return locals;
            }
        }
        self.lookup(template, kind)
    }

    fn lookup(&self, template: &str, kind: ModKind) -> Vec<(&'c StatDef, bool)> {
        let found = self.catalog.stats_by_template(template, kind);
        if found.is_empty()
            && matches!(kind, ModKind::Crafted | ModKind::Fractured | ModKind::Desecrated | ModKind::Sanctified)
        {
            return self.catalog.stats_by_template(template, ModKind::Explicit);
        }
        found
    }
}

/// Deterministic choice among several stats sharing a template: exact text first, then
/// the local/global variant fitting the item, then the lowest id.
fn break_tie<'c>(mut candidates: Vec<(&'c StatDef, bool)>, template: &str, local: bool) -> (&'c StatDef, bool) {
    let local_text = format!("{template} (Local)");
    let narrow = |c: &mut Vec<(&'c StatDef, bool)>, keep: &dyn Fn(&StatDef) -> bool| {
        if c.iter().any(|(s, _)| keep(s)) {
            c.retain(|(s, _)| keep(s));
        }
    };
    narrow(&mut candidates, &|s| s.text == template || s.text == local_text);
    narrow(&mut candidates, &|s| is_local(s) == local);
    candidates.sort_by(|a, b| a.0.id.cmp(&b.0.id));
    candidates[0]
}

/// `an additional Arrow` / `a Projectile` -> `# additional Arrows`; returns the new template
/// and the index of the inserted `#` among the template's placeholders.
fn pluralise_additional(template: &str) -> Option<(String, usize)> {
    static SINGULAR: LazyLock<Regex> =
        LazyLock::new(|| Regex::new(r"\b(?:an|a) (?:additional )?([A-Z][A-Za-z]*)\b").expect("singular regex"));
    let caps = SINGULAR.captures(template)?;
    let whole = caps.get(0)?;
    let noun = &caps[1];
    let index = template[..whole.start()].matches('#').count();
    let plural = format!("{}# additional {noun}s{}", &template[..whole.start()], &template[whole.end()..]);
    Some((plural, index))
}

/// Local stats the trade API does not mark with "(Local)": they share their text with a global
/// stat, so text alone cannot tell them apart. Matched by the stat hash after the group prefix
/// (`explicit.`, `desecrated.`, `fractured.`, `rune.` ...). Verified against live trade2 searches
/// on 2026-11-06, e.g. `#% increased Spirit`: `stat_3984865854` is on 10000+ sceptres,
/// `stat_1416406066` on none.
const UNMARKED_LOCAL_STATS: &[&str] = &["stat_3984865854"];

fn is_local(stat: &StatDef) -> bool {
    stat.text.ends_with("(Local)")
        || stat.id.split_once('.').is_some_and(|(_, hash)| UNMARKED_LOCAL_STATS.contains(&hash))
}

/// Weapons and armour pieces use local variants of damage / defence stats.
pub(crate) fn prefers_local(class: &ItemClass) -> bool {
    matches!(
        class,
        ItemClass::Weapon(_)
            | ItemClass::BodyArmour
            | ItemClass::Helmet
            | ItemClass::Gloves
            | ItemClass::Boots
            | ItemClass::Shield
            | ItemClass::Buckler
            | ItemClass::Focus
    )
}

impl std::fmt::Debug for dyn Catalog + '_ {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("dyn Catalog")
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use proptest::prelude::*;

    use super::*;
    use crate::catalog::{BaseDef, StatAlias, StaticDef, UniqueDef};

    /// Minimal in-memory catalog for tests.
    #[derive(Debug, Default)]
    pub(crate) struct FakeCatalog {
        pub stats: Vec<StatDef>,
        pub bases: Vec<BaseDef>,
        pub uniques: Vec<UniqueDef>,
        pub statics: Vec<StaticDef>,
    }

    fn kind_prefix(kind: ModKind) -> &'static str {
        match kind {
            ModKind::Implicit => "implicit",
            ModKind::Explicit => "explicit",
            ModKind::Enchant => "enchant",
            ModKind::Rune => "rune",
            ModKind::Crafted => "crafted",
            ModKind::Fractured => "fractured",
            ModKind::Desecrated => "desecrated",
            ModKind::Sanctified => "sanctified",
            ModKind::Skill => "skill",
        }
    }

    impl Catalog for FakeCatalog {
        fn stats_by_template(&self, template: &str, kind: ModKind) -> Vec<(&StatDef, bool)> {
            self.stats
                .iter()
                .filter(|s| s.kind == kind_prefix(kind))
                .filter_map(|s| {
                    if s.text == template {
                        Some((s, false))
                    } else {
                        s.aliases.iter().find(|a| a.text == template).map(|a| (s, a.negate))
                    }
                })
                .collect()
        }
        fn base(&self, name: &str) -> Option<&BaseDef> {
            self.bases.iter().find(|b| b.name == name)
        }
        fn base_in_magic_name(&self, full_name: &str) -> Option<&BaseDef> {
            self.bases.iter().filter(|b| full_name.contains(&b.name)).max_by_key(|b| b.name.len())
        }
        fn unique(&self, name: &str) -> Option<&UniqueDef> {
            self.uniques.iter().find(|u| u.name == name)
        }
        fn static_item(&self, text: &str) -> Option<&StaticDef> {
            self.statics.iter().find(|s| s.text == text)
        }
    }

    pub(crate) fn stat(id: &str, text: &str, kind: &str) -> StatDef {
        StatDef { id: id.into(), text: text.into(), kind: kind.into(), aliases: vec![] }
    }

    fn aliased(id: &str, text: &str, alias: &str) -> StatDef {
        let mut s = stat(id, text, "explicit");
        s.aliases.push(StatAlias { text: alias.into(), negate: false });
        s
    }

    pub(crate) fn fake_catalog() -> FakeCatalog {
        let mut increased_ms = stat("explicit.stat_2250533757", "#% increased Movement Speed", "explicit");
        increased_ms.aliases.push(StatAlias { text: "#% reduced Movement Speed".into(), negate: true });
        FakeCatalog {
            stats: vec![
                stat("explicit.stat_3299347043", "+# to maximum Life", "explicit"),
                stat("explicit.stat_3372524247", "+#% to Fire Resistance", "explicit"),
                stat("explicit.stat_4220027924", "+#% to Cold Resistance", "explicit"),
                stat("implicit.stat_3372524247", "+#% to Fire Resistance", "implicit"),
                stat("rune.stat_3372524247", "+#% to Fire Resistance", "rune"),
                stat("explicit.stat_1940865751", "Adds # to # Physical Damage (Local)", "explicit"),
                stat("explicit.stat_3032590688", "Adds # to # Physical Damage to Attacks", "explicit"),
                stat("explicit.stat_2223678961", "Adds # to # Physical Damage", "explicit"),
                stat("explicit.stat_dup_a", "#% increased Rarity of Items found", "explicit"),
                stat("explicit.stat_dup_b", "#% increased Rarity of Items found", "explicit"),
                increased_ms,
                stat("explicit.stat_arrows", "Bow Attacks fire # additional Arrows", "explicit"),
                aliased("explicit.stat_attr_a", "# to all Attributes", "+# to all Attributes"),
                stat("explicit.stat_attr_b", "+# to all Attributes", "explicit"),
                aliased("explicit.stat_spirit_a", "#% increased Spirit (Local)", "#% increased Spirit"),
                aliased("explicit.stat_spirit_b", "#% increased Spirit", "#% increased Spirit"),
            ],
            bases: vec![
                BaseDef { name: "Hunter Hood".into(), category: "armour.helmet".into(), item_class: "Helmets".into() },
                BaseDef {
                    name: "Expert Hunter Hood".into(),
                    category: "armour.helmet".into(),
                    item_class: "Helmets".into(),
                },
            ],
            uniques: vec![UniqueDef { name: "Goldrim".into(), base_type: "Felt Cap".into() }],
            statics: vec![StaticDef {
                id: "divine".into(),
                text: "Divine Orb".into(),
                group: "Currency".into(),
                image: None,
            }],
        }
    }

    #[test]
    fn templatize_examples() {
        assert_eq!(templatize("+42 to maximum Life"), "+# to maximum Life");
        assert_eq!(templatize("Adds 10 to 1,043 Physical Damage"), "Adds # to # Physical Damage");
        assert_eq!(templatize("-4.5% to Cold Resistance"), "-#% to Cold Resistance");
        assert_eq!(templatize("  Grants Skill: Level 12 Raise Zombie "), "Grants Skill: Level # Raise Zombie");
        assert_eq!(templatize("no numbers"), "no numbers");
    }

    #[test]
    fn values_examples() {
        assert_eq!(extract_values("Adds 10 to 1,043 Physical Damage"), vec![10.0, 1043.0]);
        assert_eq!(extract_values("+4.40% to x"), vec![4.4]);
        assert!(extract_values("none").is_empty());
    }

    fn item_with(class: ItemClass, rarity: Rarity, base: &str, lines: &[(&str, ModKind)]) -> ParsedItem {
        let text = format!("Item Class: x\nRarity: Rare\n{base}");
        let mut item = crate::parser::parse_item(&text).expect("parses");
        item.class = class;
        item.rarity = rarity;
        item.base_type = base.into();
        item.name = None;
        item.modifiers = lines
            .iter()
            .map(|(l, k)| crate::item::Modifier {
                text: (*l).into(),
                template: templatize(l),
                kind: *k,
                values: extract_values(l),
                stat_id: None,
                advanced: None,
            })
            .collect();
        item
    }

    #[test]
    fn matches_kinds_aliases_and_locals() {
        let cat = fake_catalog();
        let m = StatMatcher::new(&cat);
        assert_eq!(format!("{:?}", m.catalog()), "dyn Catalog");
        let mut item = item_with(
            ItemClass::Weapon("Bows".into()),
            Rarity::Rare,
            "Recurve Bow",
            &[
                ("+42 to maximum Life", ModKind::Explicit),
                ("+20% to Fire Resistance", ModKind::Implicit),
                ("+10% to Fire Resistance", ModKind::Rune),
                ("+12% to Fire Resistance", ModKind::Fractured),
                ("-10% to Cold Resistance", ModKind::Explicit),
                ("10% reduced Movement Speed", ModKind::Explicit),
                ("Adds 3 to 7 Physical Damage", ModKind::Explicit),
                ("20% increased Rarity of Items found", ModKind::Explicit),
                ("Totally unknown", ModKind::Explicit),
                ("Bow Attacks fire an additional Arrow", ModKind::Explicit),
                ("+10 to all Attributes", ModKind::Explicit),
                ("Has a Strange thing", ModKind::Explicit),
            ],
        );
        let out = m.match_item(&mut item);
        let ids: Vec<_> = item.modifiers.iter().map(|m| m.stat_id.as_deref()).collect();
        assert_eq!(
            ids,
            vec![
                Some("explicit.stat_3299347043"),
                Some("implicit.stat_3372524247"),
                Some("rune.stat_3372524247"),
                Some("explicit.stat_3372524247"),
                Some("explicit.stat_4220027924"),
                Some("explicit.stat_2250533757"),
                Some("explicit.stat_1940865751"),
                Some("explicit.stat_dup_a"),
                None,
                Some("explicit.stat_arrows"),
                Some("explicit.stat_attr_b"),
                None,
            ]
        );
        assert_eq!(out[4], MatchOutcome::Matched { stat_id: "explicit.stat_4220027924".into(), negated: true });
        assert_eq!(item.modifiers[4].values, vec![-10.0]);
        assert_eq!(item.modifiers[5].values, vec![-10.0]);
        assert_eq!(out[8], MatchOutcome::Unmatched);
        assert_eq!(item.modifiers[9].values, vec![1.0]);
        assert_eq!(out[11], MatchOutcome::Unmatched);

        // On jewellery the global variant wins.
        let mut ring = item_with(
            ItemClass::Ring,
            Rarity::Rare,
            "Ruby Ring",
            &[("Adds 3 to 7 Physical Damage", ModKind::Explicit), ("30% increased Spirit", ModKind::Explicit)],
        );
        m.match_item(&mut ring);
        assert_eq!(ring.modifiers[0].stat_id.as_deref(), Some("explicit.stat_2223678961"));
        assert_eq!(ring.modifiers[1].stat_id.as_deref(), Some("explicit.stat_spirit_b"));
    }

    #[test]
    fn resolves_magic_base() {
        let cat = fake_catalog();
        let m = StatMatcher::new(&cat);
        let mut item = item_with(ItemClass::Helmet, Rarity::Magic, "Hale Expert Hunter Hood of the Kiln", &[]);
        m.match_item(&mut item);
        assert_eq!(item.base_type, "Expert Hunter Hood");
        assert_eq!(item.name.as_deref(), Some("Hale Expert Hunter Hood of the Kiln"));
        // Already a bare base: untouched.
        let mut bare = item_with(ItemClass::Helmet, Rarity::Magic, "Hunter Hood", &[]);
        m.match_item(&mut bare);
        assert_eq!(bare.name, None);
        // Exercise the remaining fake-catalog lookups.
        assert!(cat.base("Hunter Hood").is_some());
        assert!(cat.unique("Goldrim").is_some());
        assert!(cat.static_item("Divine Orb").is_some());
    }

    #[test]
    fn pluralise_examples() {
        assert_eq!(
            pluralise_additional("Bow Attacks fire an additional Arrow"),
            Some(("Bow Attacks fire # additional Arrows".into(), 0))
        );
        assert_eq!(
            pluralise_additional("#% chance to fire a Projectile"),
            Some(("#% chance to fire # additional Projectiles".into(), 1))
        );
        assert_eq!(pluralise_additional("+# to maximum Life"), None);
    }

    proptest! {
        #[test]
        fn templatize_is_idempotent(s in ".{0,80}") {
            let once = templatize(&s);
            prop_assert_eq!(templatize(&once), once.clone());
            prop_assert!(!once.chars().any(|c| c.is_ascii_digit()));
        }
    }
}
