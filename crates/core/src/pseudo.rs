//! Pseudo stats: the trade site's totals (`pseudo.pseudo_total_*`) computed from an item's mods,
//! so a rare with resistances or life spread over several mods is searched by its totals.
use crate::item::{ItemClass, ModKind, Modifier};
use crate::query::{StatFilter, widen_bounds};

/// One pseudo stat RKL builds, in display order.
struct Pseudo {
    id: &'static str,
    /// Display template, `#` replaced by the total.
    text: &'static str,
    /// Enabled by default (and the mods it sums disabled).
    enabled: bool,
}

const FIRE: Pseudo =
    Pseudo { id: "pseudo.pseudo_total_fire_resistance", text: "+#% total to Fire Resistance", enabled: false };
const COLD: Pseudo =
    Pseudo { id: "pseudo.pseudo_total_cold_resistance", text: "+#% total to Cold Resistance", enabled: false };
const LIGHTNING: Pseudo = Pseudo {
    id: "pseudo.pseudo_total_lightning_resistance",
    text: "+#% total to Lightning Resistance",
    enabled: false,
};
const ELEMENTAL: Pseudo =
    Pseudo { id: "pseudo.pseudo_total_elemental_resistance", text: "+#% total Elemental Resistance", enabled: true };
const CHAOS: Pseudo =
    Pseudo { id: "pseudo.pseudo_total_chaos_resistance", text: "+#% total to Chaos Resistance", enabled: true };
const LIFE: Pseudo = Pseudo { id: "pseudo.pseudo_total_life", text: "+# total maximum Life", enabled: true };
const MANA: Pseudo = Pseudo { id: "pseudo.pseudo_total_mana", text: "+# total maximum Mana", enabled: false };
const ES: Pseudo =
    Pseudo { id: "pseudo.pseudo_total_energy_shield", text: "+# total maximum Energy Shield", enabled: true };
const STR: Pseudo = Pseudo { id: "pseudo.pseudo_total_strength", text: "+# total to Strength", enabled: false };
const DEX: Pseudo = Pseudo { id: "pseudo.pseudo_total_dexterity", text: "+# total to Dexterity", enabled: false };
const INT: Pseudo = Pseudo { id: "pseudo.pseudo_total_intelligence", text: "+# total to Intelligence", enabled: false };
const MOVE: Pseudo =
    Pseudo { id: "pseudo.pseudo_increased_movement_speed", text: "#% increased Movement Speed", enabled: true };

const ORDER: [&Pseudo; 12] = [&ELEMENTAL, &CHAOS, &LIFE, &ES, &MOVE, &MANA, &FIRE, &COLD, &LIGHTNING, &STR, &DEX, &INT];

/// What one mod template adds to which pseudo stats (`(pseudo, multiplier)`).
fn contributions(template: &str, class: &ItemClass) -> &'static [(&'static Pseudo, f64)] {
    match template {
        "+#% to Fire Resistance" => &[(&FIRE, 1.0), (&ELEMENTAL, 1.0)],
        "+#% to Cold Resistance" => &[(&COLD, 1.0), (&ELEMENTAL, 1.0)],
        "+#% to Lightning Resistance" => &[(&LIGHTNING, 1.0), (&ELEMENTAL, 1.0)],
        "+#% to all Elemental Resistances" => &[(&FIRE, 1.0), (&COLD, 1.0), (&LIGHTNING, 1.0), (&ELEMENTAL, 3.0)],
        "+#% to Fire and Cold Resistances" => &[(&FIRE, 1.0), (&COLD, 1.0), (&ELEMENTAL, 2.0)],
        "+#% to Fire and Lightning Resistances" => &[(&FIRE, 1.0), (&LIGHTNING, 1.0), (&ELEMENTAL, 2.0)],
        "+#% to Cold and Lightning Resistances" => &[(&COLD, 1.0), (&LIGHTNING, 1.0), (&ELEMENTAL, 2.0)],
        "+#% to Chaos Resistance" => &[(&CHAOS, 1.0)],
        "+# to maximum Life" => &[(&LIFE, 1.0)],
        "+# to maximum Mana" => &[(&MANA, 1.0)],
        // On armour and weapons flat energy shield is the item's own defence (local).
        "+# to maximum Energy Shield" if !crate::matcher::prefers_local(class) => &[(&ES, 1.0)],
        "+# to Strength" => &[(&STR, 1.0)],
        "+# to Dexterity" => &[(&DEX, 1.0)],
        "+# to Intelligence" => &[(&INT, 1.0)],
        "+# to all Attributes" => &[(&STR, 1.0), (&DEX, 1.0), (&INT, 1.0)],
        "+# to Strength and Dexterity" => &[(&STR, 1.0), (&DEX, 1.0)],
        "+# to Strength and Intelligence" => &[(&STR, 1.0), (&INT, 1.0)],
        "+# to Dexterity and Intelligence" => &[(&DEX, 1.0), (&INT, 1.0)],
        "#% increased Movement Speed" => &[(&MOVE, 1.0)],
        _ => &[],
    }
}

/// Pseudo stat rows for `mods`, plus the indices of the mods they sum (which the caller turns
/// off by default when the pseudo row that covers them starts enabled).
pub fn pseudo_filters(mods: &[Modifier], class: &ItemClass, widen_pct: f64) -> (Vec<StatFilter>, Vec<usize>) {
    let mut totals: Vec<(&Pseudo, f64, Vec<usize>)> = Vec::new();
    for (i, m) in mods.iter().enumerate() {
        if m.kind == ModKind::Skill {
            continue;
        }
        let Some(value) = m.values.first().copied() else { continue };
        for (p, mult) in contributions(&m.template, class) {
            match totals.iter_mut().find(|(q, _, _)| q.id == p.id) {
                Some((_, sum, from)) => {
                    *sum += value * mult;
                    from.push(i);
                }
                None => totals.push((p, value * mult, vec![i])),
            }
        }
    }
    let mut covered = Vec::new();
    let mut rows = Vec::new();
    for p in ORDER {
        let Some((_, total, from)) = totals.iter().find(|(q, _, _)| q.id == p.id) else { continue };
        // A movement speed penalty is not something to search for.
        if p.id == MOVE.id && *total <= 0.0 {
            continue;
        }
        if p.enabled {
            covered.extend(from.iter().copied());
        }
        let total = (total * 100.0).round() / 100.0;
        let (min, max) = widen_bounds(&[total], widen_pct);
        let shown = if total.fract() == 0.0 { format!("{}", total as i64) } else { format!("{total}") };
        rows.push(StatFilter {
            stat_id: p.id.to_owned(),
            text: p.text.replacen('#', &shown, 1),
            enabled: p.enabled,
            min,
            max,
            item_values: vec![total],
            tier: None,
        });
    }
    covered.sort_unstable();
    covered.dedup();
    (rows, covered)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn m(template: &str, v: f64, kind: ModKind) -> Modifier {
        Modifier {
            text: template.replace('#', &v.to_string()),
            template: template.into(),
            kind,
            values: vec![v],
            stat_id: Some("x".into()),
            advanced: None,
        }
    }

    #[test]
    fn sums_resistances_life_and_attributes() {
        let mods = [
            m("+#% to Fire Resistance", 30.0, ModKind::Explicit),
            m("+#% to Cold Resistance", 20.0, ModKind::Implicit),
            m("+#% to all Elemental Resistances", 10.0, ModKind::Explicit),
            m("+#% to Chaos Resistance", 13.0, ModKind::Explicit),
            m("+# to maximum Life", 70.0, ModKind::Explicit),
            m("+# to maximum Life", 15.0, ModKind::Rune),
            m("+# to all Attributes", 5.0, ModKind::Implicit),
            m("+# to Strength", 12.0, ModKind::Explicit),
            m("+# to maximum Energy Shield", 40.0, ModKind::Explicit),
            m("Grants Skill: Level # Something", 18.0, ModKind::Skill),
        ];
        let (rows, covered) = pseudo_filters(&mods, &ItemClass::Ring, 10.0);
        let get = |id: &str| rows.iter().find(|r| r.stat_id == id);
        let ele = get("pseudo.pseudo_total_elemental_resistance").unwrap();
        assert_eq!(
            (ele.item_values[0], ele.text.as_str(), ele.enabled, ele.min),
            (80.0, "+80% total Elemental Resistance", true, Some(72.0))
        );
        assert_eq!(get("pseudo.pseudo_total_fire_resistance").unwrap().item_values, vec![40.0]);
        assert_eq!(get("pseudo.pseudo_total_life").unwrap().item_values, vec![85.0]);
        assert_eq!(get("pseudo.pseudo_total_strength").unwrap().item_values, vec![17.0]);
        assert!(!get("pseudo.pseudo_total_strength").unwrap().enabled);
        assert_eq!(get("pseudo.pseudo_total_energy_shield").unwrap().item_values, vec![40.0], "global ES on a ring");
        assert_eq!(rows[0].stat_id, "pseudo.pseudo_total_elemental_resistance", "display order");
        // Resistances, chaos, life and ES mods are covered by enabled rows; attributes are not.
        assert_eq!(covered, vec![0, 1, 2, 3, 4, 5, 8]);
    }

    #[test]
    fn local_energy_shield_and_unrelated_mods_add_nothing() {
        let mods = [
            m("+# to maximum Energy Shield", 40.0, ModKind::Explicit),
            m("#% increased Spirit", 30.0, ModKind::Explicit),
        ];
        let (rows, covered) = pseudo_filters(&mods, &ItemClass::Helmet, 10.0);
        assert!(rows.is_empty() && covered.is_empty());
        let (rows, _) =
            pseudo_filters(&[m("#% increased Movement Speed", -10.0, ModKind::Explicit)], &ItemClass::Boots, 10.0);
        assert!(rows.is_empty(), "no pseudo row for a movement speed penalty");
    }
}
