package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
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

func TestFilterMemoryRecordsAndAppliesPerCategoryAndRoundTrips(t *testing.T) {
	var mem FilterMemory
	if !mem.Record(testFilters("accessory.ring", statChoice{"a", false}, statChoice{"b", true})) {
		t.Fatal("first record should change")
	}
	if mem.Record(testFilters("accessory.ring", statChoice{"a", false}, statChoice{"b", true})) {
		t.Fatal("no change")
	}
	next := testFilters("accessory.ring", statChoice{"a", true}, statChoice{"b", false}, statChoice{"c", true})
	mem.Apply(next)
	var on []bool
	for _, s := range next.Stats {
		on = append(on, s.Enabled)
	}
	if want := []bool{false, true, true}; !reflect.DeepEqual(on, want) {
		t.Fatalf("unknown stats keep their default: got %v want %v", on, want)
	}
	other := testFilters("weapon.bow", statChoice{"a", true})
	mem.Apply(other)
	if !other.Stats[0].Enabled {
		t.Fatal("other categories are untouched")
	}

	path := filepath.Join(t.TempDir(), "sub", FilterMemoryFileName)
	if err := mem.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadFilterMemory(path); !reflect.DeepEqual(got, mem) {
		t.Fatalf("round trip: got %v want %v", got, mem)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadFilterMemory(path); !reflect.DeepEqual(got, FilterMemory{}) {
		t.Fatalf("garbage should load empty, got %v", got)
	}
}

func TestFilterMemoryUniquesAreNotRememberedAndTheSizeIsCapped(t *testing.T) {
	var mem FilterMemory
	unique := testFilters("accessory.ring", statChoice{"a", true})
	unique.Name = new("Andvarius")
	if mem.Record(unique) {
		t.Fatal("uniques are not remembered")
	}
	var many []statChoice
	for i := range MaxFilterMemoryPerCategory + 5 {
		many = append(many, statChoice{fmt.Sprintf("s%d", i), true})
	}
	mem.Record(testFilters("accessory.ring", many...))
	if n := len(mem.Categories["accessory.ring"]); n != MaxFilterMemoryPerCategory {
		t.Fatalf("got %d entries", n)
	}
}

// The file is shared with the Rust build: same name, same serde_json bytes (sorted keys).
func TestFilterMemoryFileFormatMatchesSerde(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FilterMemoryFileName)
	if err := (&FilterMemory{}).Save(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != `{"categories":{}}` {
		t.Fatalf("empty: %s", b)
	}
	mem := FilterMemory{Categories: map[string]map[string]bool{
		"weapon.bow":     {"z": true, "a": false},
		"accessory.ring": {"explicit.stat_1": true},
	}}
	if err := mem.Save(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	want := `{"categories":{"accessory.ring":{"explicit.stat_1":true},"weapon.bow":{"a":false,"z":true}}}`
	if string(b) != want {
		t.Fatalf("got %s\nwant %s", b, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "filter-memory.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
	// serde requires the field: a file without it starts empty.
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadFilterMemory(path); got.Categories != nil {
		t.Fatalf("missing categories should load empty, got %v", got)
	}
}
