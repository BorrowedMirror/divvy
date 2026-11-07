package core

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func opts() *QueryOptions {
	return &QueryOptions{League: "Standard", Status: StatusOnline, WidenPct: 10, EnableAllStats: false}
}

func rareBow(t *testing.T) *ParsedItem {
	text := "Item Class: Bows\nRarity: Rare\nHate Thirst\nRecurve Bow\n--------\nItem Level: 80\n--------\n+20% to Fire Resistance (implicit)\n--------\n+42 to maximum Life\n+12% to Fire Resistance\n+31% to Cold Resistance\nAdds 3 to 8 Physical Damage\n10% reduced Movement Speed\n--------\nCorrupted"
	item := mustParse(t, text)
	NewStatMatcher(newFakeCatalog()).MatchItem(item)
	return item
}

func statByText(t *testing.T, f *FilterSet, text string) *StatFilter {
	t.Helper()
	for i := range f.Stats {
		if f.Stats[i].Text == text {
			return &f.Stats[i]
		}
	}
	t.Fatalf("no stat %q in %+v", text, f.Stats)
	return nil
}

// toValue is `serde_json::to_value`: the serde JSON decoded into generic values.
func toValue(t testing.TB, v any) any {
	t.Helper()
	b, err := MarshalSerde(v)
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

// at indexes a generic JSON value by object keys (string) and array indices (int); nil when
// missing, like serde_json's `Index`.
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func TestRareDefaults(t *testing.T) {
	f := BuildFilters(rareBow(t), opts())
	assert.Equal(t, RouteSearch, f.Route)
	assert.Equal(t, StatusOnline, f.Status)
	assert.Equal(t, new("Recurve Bow"), f.BaseType)
	assert.Nil(t, f.Name)
	assert.Equal(t, new("weapon.bow"), f.Category)
	assert.Equal(t, new("nonunique"), f.Rarity)
	// 6 mods plus pseudo totals: elemental resistance and life (enabled), fire and cold.
	require.Len(t, f.Stats, 10)
	var ids []string
	for _, s := range f.Stats[:4] {
		ids = append(ids, s.StatID)
	}
	assert.Equal(t, []string{
		"pseudo.pseudo_total_elemental_resistance",
		"pseudo.pseudo_total_life",
		"pseudo.pseudo_total_fire_resistance",
		"pseudo.pseudo_total_cold_resistance",
	}, ids)
	assert.Equal(t, new(56.0), statByText(t, &f, "+63% total Elemental Resistance").Min)
	assert.True(t, statByText(t, &f, "+63% total Elemental Resistance").Enabled &&
		statByText(t, &f, "+42 total maximum Life").Enabled)
	assert.False(t, statByText(t, &f, "+20% to Fire Resistance").Enabled, "implicit disabled")
	assert.False(t, statByText(t, &f, "+42 to maximum Life").Enabled, "covered by the life total")
	assert.False(t, statByText(t, &f, "+31% to Cold Resistance").Enabled, "covered by the resistance total")
	assert.Equal(t, new(37.0), statByText(t, &f, "+42 to maximum Life").Min)
	assert.Equal(t, new(4.95), statByText(t, &f, "Adds 3 to 8 Physical Damage").Min) // avg of 3 and 8, fractional
	assert.True(t, statByText(t, &f, "Adds 3 to 8 Physical Damage").Enabled)
	for _, s := range f.Stats {
		assert.NotContains(t, s.StatID, "movement", "no pseudo row for a penalty")
	}
	assert.Equal(t, new(true), f.Misc.Corrupted)
	assert.False(t, f.Misc.ItemLevel.Enabled)

	o := opts()
	o.EnableAllStats = true
	o.Status = StatusAny
	all := BuildFilters(rareBow(t), o)
	enabled := 0
	for _, s := range all.Stats {
		if s.Enabled {
			enabled++
		}
	}
	assert.Equal(t, 4, enabled, "pseudo totals + uncovered mods")
	assert.Equal(t, StatusAny, all.Status)
}

func TestRewidenRecomputes(t *testing.T) {
	f := BuildFilters(rareBow(t), opts())
	f.Rewiden(0)
	assert.Less(t, math.Abs(f.WidenPct), 2.220446049250313e-16)
	assert.Equal(t, new(42.0), statByText(t, &f, "+42 to maximum Life").Min)
	f.Rewiden(50)
	assert.Equal(t, new(21.0), statByText(t, &f, "+42 to maximum Life").Min)
	assert.Equal(t, new(21.0), statByText(t, &f, "+42 total maximum Life").Min, "pseudo rows rewiden too")
	// Negated stat ("10% reduced ..." -> -10): more negative is better, so it gets an upper
	// bound widened towards zero instead of a minimum that would match any item.
	ms := statByText(t, &f, "10% reduced Movement Speed")
	assert.Equal(t, []float64{-10}, ms.ItemValues)
	assert.Nil(t, ms.Min)
	assert.Equal(t, new(-5.0), ms.Max)
}

func TestTradeQueryShape(t *testing.T) {
	f := BuildFilters(rareBow(t), opts())
	f.Status = StatusSecurable
	f.Misc.Sockets = RangeFilter{Enabled: true, Min: new(2.0)}
	f.Misc.Identified = new(false)
	q := toValue(t, f.ToTradeQuery())
	assert.Equal(t, "securable", at(q, "query", "status", "option"))
	assert.Equal(t, "Recurve Bow", at(q, "query", "type"))
	assert.Equal(t, "and", at(q, "query", "stats", 0, "type"))
	filters := at(q, "query", "stats", 0, "filters").([]any)
	byID := func(id string) any {
		for _, f := range filters {
			if at(f, "id") == id {
				return f
			}
		}
		t.Fatalf("no filter %s", id)
		return nil
	}
	assert.Equal(t, 37.0, at(byID("pseudo.pseudo_total_life"), "value", "min"))
	assert.Equal(t, false, at(byID("pseudo.pseudo_total_life"), "disabled"))
	assert.Equal(t, true, at(filters[2], "disabled"), "fire total starts off")
	assert.Equal(t, "weapon.bow", at(q, "query", "filters", "type_filters", "filters", "category", "option"))
	assert.Equal(t, "true", at(q, "query", "filters", "misc_filters", "filters", "corrupted", "option"))
	assert.Equal(t, "false", at(q, "query", "filters", "misc_filters", "filters", "identified", "option"))
	assert.Equal(t, 2.0, at(q, "query", "filters", "equipment_filters", "filters", "rune_sockets", "min"))
	_, hasMap := at(q, "query", "filters").(map[string]any)["map_filters"]
	assert.False(t, hasMap)
	assert.Equal(t, "asc", at(q, "sort", "price"))
}

func TestUniqueGemWaystoneCurrency(t *testing.T) {
	unique := mustParse(t, "Item Class: Helmets\nRarity: Unique\nGoldrim\nFelt Cap\n--------\nItem Level: 50\n--------\n+42 to maximum Life")
	NewStatMatcher(newFakeCatalog()).MatchItem(unique)
	f := BuildFilters(unique, opts())
	assert.Equal(t, new("Goldrim"), f.Name)
	assert.Equal(t, new("Felt Cap"), f.BaseType)
	assert.Equal(t, new("unique"), f.Rarity)
	for _, s := range f.Stats {
		assert.False(t, s.Enabled)
	}
	assert.Equal(t, new(false), f.Misc.Corrupted)
	q := f.ToTradeQuery()
	assert.Equal(t, "Goldrim", at(q.Query, "name"))

	gem := mustParse(t, "Item Class: Skill Gems\nRarity: Gem\nFireball\n--------\nLevel: 20 (Max)\nQuality: +20%")
	f = BuildFilters(gem, opts())
	assert.True(t, f.Misc.GemLevel.Enabled && f.Misc.Quality.Enabled)
	assert.Nil(t, f.Rarity)
	v := toValue(t, f.ToTradeQuery())
	assert.Equal(t, 20.0, at(v, "query", "filters", "misc_filters", "filters", "gem_level", "min"))
	assert.Equal(t, 20.0, at(v, "query", "filters", "type_filters", "filters", "quality", "min"))

	way := mustParse(t, "Item Class: Waystones\nRarity: Normal\nWaystone (Tier 15)")
	wf := BuildFilters(way, opts())
	v = toValue(t, wf.ToTradeQuery())
	assert.Equal(t, map[string]any{"min": 15.0, "max": 15.0}, at(v, "query", "filters", "map_filters", "filters", "map_tier"))

	orb := mustParse(t, "Item Class: Stackable Currency\nRarity: Currency\nDivine Orb")
	assert.Equal(t, RouteExchange, BuildFilters(orb, opts()).Route)

	magic := mustParse(t, "Item Class: Rings\nRarity: Magic\nHale Ruby Ring of the Kiln\n--------\nItem Level: 40")
	f = BuildFilters(magic, opts())
	assert.Nil(t, f.BaseType)
	assert.True(t, f.Misc.ItemLevel.Enabled)
}

func TestRangeJSONEdges(t *testing.T) {
	_, ok := rangeJSON(&RangeFilter{Enabled: true})
	assert.False(t, ok)
	_, ok = rangeJSON(&RangeFilter{Enabled: false, Min: new(1.0)})
	assert.False(t, ok)
	v, ok := rangeJSON(&RangeFilter{Enabled: true, Max: new(3.0)})
	assert.True(t, ok)
	assert.Equal(t, map[string]any{"max": 3.0}, v)
	minV, maxV := WidenBounds(nil, 10)
	assert.Nil(t, minV)
	assert.Nil(t, maxV)
}

func TestWidenNeverExceedsValue(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Float64Range(-10_000, 10_000).Draw(t, "v")
		pct := rapid.Float64Range(-50, 200).Draw(t, "pct")
		integral := rapid.Bool().Draw(t, "integral")
		if integral {
			v = math.Round(v)
		}
		minV := WidenMin(v, pct, integral)
		if !(minV <= v) {
			t.Fatalf("min %v > v %v", minV, v)
		}
		// Never widened beyond 100% (+ rounding slack).
		if !(minV >= v-math.Abs(v)-1) {
			t.Fatalf("min %v widened beyond 100%% of %v", minV, v)
		}
	})
}

func TestReducedStatsGetAnUpperBound(t *testing.T) {
	minV, maxV := WidenBounds([]float64{-30}, 10)
	assert.Nil(t, minV)
	assert.Equal(t, new(-27.0), maxV)
	minV, maxV = WidenBounds([]float64{55}, 10)
	assert.Equal(t, new(49.0), minV)
	assert.Nil(t, maxV)
	minV, maxV = WidenBounds([]float64{}, 10)
	assert.Nil(t, minV)
	assert.Nil(t, maxV)
	assert.Less(t, math.Abs(WidenMaxNegative(-2.5, 10, false)-(-2.25)), 1e-9)
}

func TestDesecratedModsSearchAsExplicit(t *testing.T) {
	assert.Equal(t, "explicit.stat_1050105434", SearchStatID("desecrated.stat_1050105434"))
	assert.Equal(t, "fractured.stat_1", SearchStatID("fractured.stat_1"))
}

func TestModsWithTheSameStatMergeIntoOneSummedFilter(t *testing.T) {
	row := func(id, text string, v float64, enabled bool, tier uint8) templatedRow {
		return templatedRow{
			row: StatFilter{
				StatID: id, Text: text, Enabled: enabled, ItemValues: []float64{v}, Tiers: []uint8{tier},
			},
			template: "#% increased Spirit",
		}
	}
	merged := mergeSameStats([]templatedRow{
		row("explicit.stat_1416406066", "55% increased Spirit", 55, false, 3),
		row("explicit.stat_9", "+20 to maximum Mana", 20, true, 2),
		row("explicit.stat_1416406066", "13% increased Spirit", 13, true, 7),
	}, 10)
	require.Len(t, merged, 2)
	assert.Equal(t, "68% increased Spirit", merged[0].Text)
	assert.Equal(t, []float64{68}, merged[0].ItemValues)
	assert.Equal(t, new(61.0), merged[0].Min)
	assert.True(t, merged[0].Enabled)
	assert.Equal(t, []uint8{3, 7}, merged[0].Tiers, "summed rows keep each tier")
	assert.Equal(t, []uint8{2}, merged[1].Tiers)
	assert.Equal(t, "Adds 1.5 to 3 Damage", renderTemplate("Adds # to # Damage", []float64{1.5, 3}))
	assert.Equal(t, "1 and #", renderTemplate("# and #", []float64{1}))
}
