package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pm(template string, v float64, kind ModKind) Modifier {
	return Modifier{
		Text:     strings.ReplaceAll(template, "#", formatF64Display(v)),
		Template: template,
		Kind:     kind,
		Values:   []float64{v},
		StatID:   new("x"),
	}
}

func TestSumsResistancesLifeAndAttributes(t *testing.T) {
	mods := []Modifier{
		pm("+#% to Fire Resistance", 30, ModExplicit),
		pm("+#% to Cold Resistance", 20, ModImplicit),
		pm("+#% to all Elemental Resistances", 10, ModExplicit),
		pm("+#% to Chaos Resistance", 13, ModExplicit),
		pm("+# to maximum Life", 70, ModExplicit),
		pm("+# to maximum Life", 15, ModRune),
		pm("+# to all Attributes", 5, ModImplicit),
		pm("+# to Strength", 12, ModExplicit),
		pm("+# to maximum Energy Shield", 40, ModExplicit),
		pm("Grants Skill: Level # Something", 18, ModSkill),
	}
	rows, covered := PseudoFilters(mods, ClassRing.Class(), 10)
	get := func(id string) *StatFilter {
		i := slices.IndexFunc(rows, func(r StatFilter) bool { return r.StatID == id })
		require.GreaterOrEqual(t, i, 0, id)
		return &rows[i]
	}
	ele := get("pseudo.pseudo_total_elemental_resistance")
	assert.Equal(t, 80.0, ele.ItemValues[0])
	assert.Equal(t, "+80% total Elemental Resistance", ele.Text)
	assert.True(t, ele.Enabled)
	assert.Equal(t, new(72.0), ele.Min)
	assert.Equal(t, []float64{40}, get("pseudo.pseudo_total_fire_resistance").ItemValues)
	assert.Equal(t, []float64{85}, get("pseudo.pseudo_total_life").ItemValues)
	assert.Equal(t, []float64{17}, get("pseudo.pseudo_total_strength").ItemValues)
	assert.False(t, get("pseudo.pseudo_total_strength").Enabled)
	assert.Equal(t, []float64{40}, get("pseudo.pseudo_total_energy_shield").ItemValues, "global ES on a ring")
	assert.Equal(t, "pseudo.pseudo_total_elemental_resistance", rows[0].StatID, "display order")
	// Resistances, chaos, life and ES mods are covered by enabled rows; attributes are not.
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 8}, covered)
}

func TestLocalEnergyShieldAndUnrelatedModsAddNothing(t *testing.T) {
	mods := []Modifier{
		pm("+# to maximum Energy Shield", 40, ModExplicit),
		pm("#% increased Spirit", 30, ModExplicit),
	}
	rows, covered := PseudoFilters(mods, ClassHelmet.Class(), 10)
	assert.True(t, len(rows) == 0 && len(covered) == 0)
	rows, _ = PseudoFilters([]Modifier{pm("#% increased Movement Speed", -10, ModExplicit)}, ClassBoots.Class(), 10)
	assert.Empty(t, rows, "no pseudo row for a movement speed penalty")
}
