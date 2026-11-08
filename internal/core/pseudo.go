package core

// Pseudo stats: the trade site's totals (`pseudo.pseudo_total_*`) computed from an item's
// mods, so a rare with resistances or life spread over several mods is searched by its
// totals.

import (
	"math"
	"slices"
	"strings"
)

// pseudo is one pseudo stat RKL builds, in display order.
type pseudo struct {
	id string
	// Display template, `#` replaced by the total.
	text string
	// Enabled by default (and the mods it sums disabled).
	enabled bool
}

var (
	pseudoFire      = &pseudo{"pseudo.pseudo_total_fire_resistance", "+#% total to Fire Resistance", false}
	pseudoCold      = &pseudo{"pseudo.pseudo_total_cold_resistance", "+#% total to Cold Resistance", false}
	pseudoLightning = &pseudo{"pseudo.pseudo_total_lightning_resistance", "+#% total to Lightning Resistance", false}
	pseudoElemental = &pseudo{"pseudo.pseudo_total_elemental_resistance", "+#% total Elemental Resistance", true}
	pseudoChaos     = &pseudo{"pseudo.pseudo_total_chaos_resistance", "+#% total to Chaos Resistance", true}
	pseudoLife      = &pseudo{"pseudo.pseudo_total_life", "+# total maximum Life", true}
	pseudoMana      = &pseudo{"pseudo.pseudo_total_mana", "+# total maximum Mana", false}
	pseudoES        = &pseudo{"pseudo.pseudo_total_energy_shield", "+# total maximum Energy Shield", true}
	pseudoStr       = &pseudo{"pseudo.pseudo_total_strength", "+# total to Strength", false}
	pseudoDex       = &pseudo{"pseudo.pseudo_total_dexterity", "+# total to Dexterity", false}
	pseudoInt       = &pseudo{"pseudo.pseudo_total_intelligence", "+# total to Intelligence", false}
	pseudoMove      = &pseudo{"pseudo.pseudo_increased_movement_speed", "#% increased Movement Speed", true}
)

var pseudoOrder = [12]*pseudo{
	pseudoElemental, pseudoChaos, pseudoLife, pseudoES, pseudoMove, pseudoMana, pseudoFire, pseudoCold,
	pseudoLightning, pseudoStr, pseudoDex, pseudoInt,
}

type contribution struct {
	p    *pseudo
	mult float64
}

// contributions says what one mod template adds to which pseudo stats.
func contributions(template string, class ItemClass) []contribution {
	switch template {
	case "+#% to Fire Resistance":
		return []contribution{{pseudoFire, 1}, {pseudoElemental, 1}}
	case "+#% to Cold Resistance":
		return []contribution{{pseudoCold, 1}, {pseudoElemental, 1}}
	case "+#% to Lightning Resistance":
		return []contribution{{pseudoLightning, 1}, {pseudoElemental, 1}}
	case "+#% to all Elemental Resistances":
		return []contribution{{pseudoFire, 1}, {pseudoCold, 1}, {pseudoLightning, 1}, {pseudoElemental, 3}}
	case "+#% to Fire and Cold Resistances":
		return []contribution{{pseudoFire, 1}, {pseudoCold, 1}, {pseudoElemental, 2}}
	case "+#% to Fire and Lightning Resistances":
		return []contribution{{pseudoFire, 1}, {pseudoLightning, 1}, {pseudoElemental, 2}}
	case "+#% to Cold and Lightning Resistances":
		return []contribution{{pseudoCold, 1}, {pseudoLightning, 1}, {pseudoElemental, 2}}
	case "+#% to Chaos Resistance":
		return []contribution{{pseudoChaos, 1}}
	case "+# to maximum Life":
		return []contribution{{pseudoLife, 1}}
	case "+# to maximum Mana":
		return []contribution{{pseudoMana, 1}}
	case "+# to maximum Energy Shield":
		// On armour and weapons flat energy shield is the item's own defence (local).
		if !prefersLocal(class) {
			return []contribution{{pseudoES, 1}}
		}
	case "+# to Strength":
		return []contribution{{pseudoStr, 1}}
	case "+# to Dexterity":
		return []contribution{{pseudoDex, 1}}
	case "+# to Intelligence":
		return []contribution{{pseudoInt, 1}}
	case "+# to all Attributes":
		return []contribution{{pseudoStr, 1}, {pseudoDex, 1}, {pseudoInt, 1}}
	case "+# to Strength and Dexterity":
		return []contribution{{pseudoStr, 1}, {pseudoDex, 1}}
	case "+# to Strength and Intelligence":
		return []contribution{{pseudoStr, 1}, {pseudoInt, 1}}
	case "+# to Dexterity and Intelligence":
		return []contribution{{pseudoDex, 1}, {pseudoInt, 1}}
	case "#% increased Movement Speed":
		return []contribution{{pseudoMove, 1}}
	}
	return nil
}

type pseudoTotal struct {
	p    *pseudo
	sum  float64
	from []int
}

// pseudoFilters returns pseudo stat rows for mods, plus the indices of the mods they sum
// (which the caller turns off by default when the pseudo row that covers them starts
// enabled).
func pseudoFilters(mods []Modifier, class ItemClass, widenPct float64) ([]StatFilter, []int) {
	var totals []*pseudoTotal
	for i, m := range mods {
		if m.Kind == ModSkill {
			continue
		}
		if len(m.Values) == 0 {
			continue
		}
		value := m.Values[0]
		for _, c := range contributions(m.Template, class) {
			idx := slices.IndexFunc(totals, func(t *pseudoTotal) bool { return t.p.id == c.p.id })
			if idx >= 0 {
				totals[idx].sum += value * c.mult
				totals[idx].from = append(totals[idx].from, i)
			} else {
				totals = append(totals, &pseudoTotal{c.p, value * c.mult, []int{i}})
			}
		}
	}
	covered := []int{}
	rows := []StatFilter{}
	for _, p := range pseudoOrder {
		idx := slices.IndexFunc(totals, func(t *pseudoTotal) bool { return t.p.id == p.id })
		if idx < 0 {
			continue
		}
		t := totals[idx]
		// A movement speed penalty is not something to search for.
		if p.id == pseudoMove.id && t.sum <= 0 {
			continue
		}
		if p.enabled {
			covered = append(covered, t.from...)
		}
		total := math.Round(t.sum*100) / 100
		minV, maxV := widenBounds([]float64{total}, widenPct)
		rows = append(rows, StatFilter{
			StatID:     p.id,
			Text:       strings.Replace(p.text, "#", formatNumber(total), 1),
			Enabled:    p.enabled,
			Min:        minV,
			Max:        maxV,
			ItemValues: []float64{total},
			Tiers:      []int{},
		})
	}
	slices.Sort(covered)
	covered = slices.Compact(covered)
	return rows, covered
}
