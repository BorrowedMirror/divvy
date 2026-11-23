package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/core"
)

type statChoice struct {
	id string
	on bool
}

func testFilters(category string, stats ...statChoice) *core.FilterSet {
	f := &core.FilterSet{
		League:   "Standard",
		Status:   core.StatusOnline,
		BaseType: new("Ruby Ring"),
		Category: new(category),
		Rarity:   new("nonunique"),
		WidenPct: 10,
		Route:    core.RouteSearch,
	}
	for _, s := range stats {
		f.Stats = append(f.Stats, core.StatFilter{StatID: s.id, Text: s.id, Enabled: s.on})
	}
	return f
}

func enabledStats(f *core.FilterSet) []bool {
	var on []bool
	for _, s := range f.Stats {
		on = append(on, s.Enabled)
	}
	return on
}

func saveFilterMemory(t *testing.T, m *filterMemory, path string) {
	t.Helper()
	b, err := m.marshal()
	require.NoError(t, err)
	require.NoError(t, writeFileAtomic(path, b))
}

func TestFilterMemoryRecordsAndAppliesPerCategoryAndRoundTrips(t *testing.T) {
	var mem filterMemory
	assert.True(t, mem.record(testFilters("accessory.ring", statChoice{"a", false}, statChoice{"b", true})), "first record changes")
	assert.False(t, mem.record(testFilters("accessory.ring", statChoice{"a", false}, statChoice{"b", true})), "no change")

	next := testFilters("accessory.ring", statChoice{"a", true}, statChoice{"b", false}, statChoice{"c", true})
	mem.apply(next)
	assert.Equal(t, []bool{false, true, true}, enabledStats(next), "unknown stats keep their default")
	other := testFilters("weapon.bow", statChoice{"a", true})
	mem.apply(other)
	assert.True(t, other.Stats[0].Enabled, "other categories are untouched")

	path := filepath.Join(t.TempDir(), "sub", filterMemoryFile)
	saveFilterMemory(t, &mem, path)
	assert.Equal(t, mem, loadFilterMemory(path), "round trip")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
	assert.Equal(t, filterMemory{}, loadFilterMemory(path), "garbage loads empty")
}

func TestFilterMemoryUniquesAreNotRememberedAndTheSizeIsCapped(t *testing.T) {
	var mem filterMemory
	unique := testFilters("accessory.ring", statChoice{"a", true})
	unique.Name = new("Andvarius")
	assert.False(t, mem.record(unique), "uniques are not remembered")
	var many []statChoice
	for i := range maxFilterMemoryPerCategory + 5 {
		many = append(many, statChoice{fmt.Sprintf("s%d", i), true})
	}
	mem.record(testFilters("accessory.ring", many...))
	assert.Len(t, mem.Categories["accessory.ring"], maxFilterMemoryPerCategory)
}

func TestFilterMemoryFileFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filterMemoryFile)
	saveFilterMemory(t, &filterMemory{}, path)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"categories":{}}`, string(b))

	mem := filterMemory{Categories: map[string]map[string]bool{
		"weapon.bow":     {"z": true, "a": false},
		"accessory.ring": {"explicit.stat_1": true},
	}}
	saveFilterMemory(t, &mem, path)
	b, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"categories":{"accessory.ring":{"explicit.stat_1":true},"weapon.bow":{"a":false,"z":true}}}`, string(b))
	assert.Less(t, strings.Index(string(b), "accessory.ring"), strings.Index(string(b), "weapon.bow"), "sorted keys")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file left behind")

	// The field is required: a file without it starts empty.
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	assert.Nil(t, loadFilterMemory(path).Categories)
}
