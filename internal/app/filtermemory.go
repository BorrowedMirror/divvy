package app

// Remembered stat choices per item category: which stats the user had on or off the last
// time they searched an item of that category (rings, bows, ...). Stored as JSON in the cache
// directory; losing it only loses the preference.

import (
	"encoding/json/v2"
	"os"

	"github.com/BorrowedMirror/divvy/internal/core"
)

// maxFilterMemoryPerCategory is the most stat choices kept per category, so the file stays
// small.
const maxFilterMemoryPerCategory = 300

// filterMemory is the remembered stat choices. It is not safe for concurrent use.
type filterMemory struct {
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

// loadFilterMemory reads the file, or starts empty when it is missing or unreadable.
func loadFilterMemory(path string) filterMemory {
	b, err := os.ReadFile(path)
	if err != nil {
		return filterMemory{}
	}
	// `categories` is required: anything else means "start empty".
	var raw struct {
		Categories *map[string]map[string]bool `json:"categories"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || raw.Categories == nil {
		return filterMemory{}
	}
	for _, prefs := range *raw.Categories {
		if prefs == nil {
			return filterMemory{}
		}
	}
	return filterMemory{Categories: *raw.Categories}
}

// marshal encodes the memory with sorted keys.
func (m *filterMemory) marshal() ([]byte, error) {
	return json.Marshal(m, json.Deterministic(true))
}

// record remembers the on/off state of every stat in a search the user ran. It reports whether
// anything changed.
func (m *filterMemory) record(filters *core.FilterSet) bool {
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
		if len(entry) >= maxFilterMemoryPerCategory && !known {
			continue
		}
		entry[s.StatID] = s.Enabled
		if !known || prev != s.Enabled {
			changed = true
		}
	}
	return changed
}

// apply turns stats on or off as the user last had them for this category.
func (m *filterMemory) apply(filters *core.FilterSet) {
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
