package app

// Remembered stat choices per item category: which stats the user had on or off the last
// time they searched an item of that category (rings, bows, ...). Stored as JSON in the cache
// directory; losing it only loses the preference.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// MaxFilterMemoryPerCategory is the most stat choices kept per category, so the file stays
// small.
const MaxFilterMemoryPerCategory = 300

// FilterMemoryFileName is the file name in the cache directory (shared with the Rust build).
const FilterMemoryFileName = "filter-memory.json"

// FilterMemory is the remembered stat choices.
type FilterMemory struct {
	// Categories maps category -> stat id -> enabled.
	Categories map[string]map[string]bool `json:"categories"`
}

func filterCategory(filters *core.FilterSet) (string, bool) {
	// Uniques are searched by name; their stats start off on purpose.
	if filters.Name != nil {
		return "", false
	}
	if filters.Category == nil || *filters.Category == "" {
		return "", false
	}
	return *filters.Category, true
}

// LoadFilterMemory reads the file, or starts empty when it is missing or unreadable.
func LoadFilterMemory(path string) FilterMemory {
	b, err := os.ReadFile(path)
	if err != nil {
		return FilterMemory{}
	}
	// `categories` is required, as in the serde struct: anything else means "start empty".
	var raw struct {
		Categories *map[string]map[string]bool `json:"categories"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || raw.Categories == nil {
		return FilterMemory{}
	}
	for _, prefs := range *raw.Categories {
		if prefs == nil {
			return FilterMemory{}
		}
	}
	return FilterMemory{Categories: *raw.Categories}
}

// Save writes atomically (temp file + rename).
func (m *FilterMemory) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	b, err := core.MarshalSerde(m)
	if err != nil {
		return err
	}
	tmp := strings.TrimSuffix(path, filepath.Ext(path)) + ".json.tmp"
	if err := os.WriteFile(tmp, b, 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Record remembers the on/off state of every stat in a search the user ran. It reports whether
// anything changed.
func (m *FilterMemory) Record(filters *core.FilterSet) bool {
	cat, ok := filterCategory(filters)
	if !ok {
		return false
	}
	if m.Categories == nil {
		m.Categories = map[string]map[string]bool{}
	}
	entry := m.Categories[cat]
	if entry == nil {
		entry = map[string]bool{}
		m.Categories[cat] = entry
	}
	changed := false
	for _, s := range filters.Stats {
		prev, known := entry[s.StatID]
		if len(entry) >= MaxFilterMemoryPerCategory && !known {
			continue
		}
		entry[s.StatID] = s.Enabled
		if !known || prev != s.Enabled {
			changed = true
		}
	}
	return changed
}

// Apply turns stats on or off as the user last had them for this category.
func (m *FilterMemory) Apply(filters *core.FilterSet) {
	cat, ok := filterCategory(filters)
	if !ok {
		return
	}
	prefs, ok := m.Categories[cat]
	if !ok {
		return
	}
	for i := range filters.Stats {
		if on, ok := prefs[filters.Stats[i].StatID]; ok {
			filters.Stats[i].Enabled = on
		}
	}
}
