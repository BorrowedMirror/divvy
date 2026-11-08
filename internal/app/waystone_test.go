package app

import (
	"encoding/json/v2"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
)

func TestWaystoneCatalogGroupsDedupesAndAddsExtras(t *testing.T) {
	stats := []string{
		"Monsters fire # additional Projectiles",
		"Players have #% less Recovery Rate of Life and Energy Shield",
		"#% increased Pack Size in Map",
		"+# to maximum Life",
		"Monsters fire # additional Projectiles",
		"Area has patches of Shocked Ground",
	}
	c := waystoneCatalog(slices.Values(stats))
	assert.Contains(t, c, WaystoneMod{"Players have -#% to all maximum Resistances", WaystonePlayer}, "extras included")
	assert.Contains(t, c, WaystoneMod{"#% increased Pack Size in Map", WaystoneArea}, "pack size is an area mod")
	assert.NotContains(t, c, WaystoneMod{"+# to maximum Life", ""}, "ordinary item mods are left out")
	n := 0
	for _, m := range c {
		if m.Text == "Monsters fire # additional Projectiles" {
			n++
		}
	}
	assert.Equal(t, 1, n, "deduplicated")
	assert.True(t, slices.IsSortedFunc(c, compareWaystoneMods), "sorted: %v", c)
	assert.Equal(t, WaystonePlayer, c[0].Group, "groups sort in declaration order")
	assert.Equal(t, WaystoneArea, c[len(c)-1].Group, "groups sort in declaration order")
	for _, m := range config.DefaultDangerousWaystoneMods {
		_, ok := waystoneGroup(m)
		assert.True(t, ok, "%s must be in the catalogue", m)
	}
}

func TestWaystoneModGroupMatchesBindings(t *testing.T) {
	assert.Equal(t, tsUnion(t, bindingsTS(t), "WaystoneModGroup"), enumStrings(waystoneModGroups))
	var g WaystoneModGroup
	require.NoError(t, json.Unmarshal([]byte(`"monster"`), &g))
	assert.Equal(t, WaystoneMonster, g)
	for _, bad := range []string{`"Monster"`, `"area "`, `""`, `1`} {
		assert.Error(t, json.Unmarshal([]byte(bad), &g), bad)
	}
}

// tsFields returns the field names of `export type name = { ... };`, in order.
func tsFields(t *testing.T, ts, name string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?s)\nexport type ` + name + ` = \{\n(.*?)\n\};`).FindStringSubmatch(ts)
	require.NotNil(t, m, "no object type %s in bindings.ts", name)
	var out []string
	for _, f := range regexp.MustCompile(`(?m)^\t(\w+)\??: `).FindAllStringSubmatch(m[1], -1) {
		out = append(out, f[1])
	}
	return out
}

func TestWaystoneModJSON(t *testing.T) {
	assert.Equal(t, []string{"text", "group"}, tsFields(t, bindingsTS(t), "WaystoneMod"))
	mods := []WaystoneMod{
		{Text: "Players have -#% to all maximum Resistances", Group: WaystonePlayer},
		{Text: `Monsters deal <b> & "more"`, Group: WaystoneMonster},
	}
	b, err := json.Marshal(mods)
	require.NoError(t, err)
	want := `[{"text":"Players have -#% to all maximum Resistances","group":"player"},` +
		`{"text":"Monsters deal <b> & \"more\"","group":"monster"}]`
	assert.JSONEq(t, want, string(b))
	var back []WaystoneMod
	require.NoError(t, json.Unmarshal([]byte(want), &back))
	assert.Equal(t, mods, back)
	var m WaystoneMod
	require.Error(t, json.Unmarshal([]byte(`{"text":"x","group":"Area"}`), &m), "an unknown group")
	// An empty catalogue is `[]`, never null.
	b, err = json.Marshal([]WaystoneMod(nil))
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(b))
}
