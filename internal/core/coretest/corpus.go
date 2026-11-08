// Package coretest gives tests the item-text corpus: item texts exactly as the game copies
// them, in internal/core/testdata/corpus.
//
// The corpus index, index.toml, lists entries:
//
//	[[item]]
//	id = "currency/divine-orb"         # path under corpus/ without extension
//	class = "stackable_currency"       # expected item class kind (JSON `type`)
//	rarity = "currency"                # expected Rarity (snake_case)
//	advanced = false                   # copied with Ctrl+Alt+C
//	source = "hand-written from 0.3 in-game copy"
//
// Each entry has corpus/<id>.txt containing the exact clipboard text (LF line endings; the
// parser must also accept CRLF). The expected parse output is a JSON golden in
// internal/core/testdata/items.
package coretest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pelletier/go-toml/v2"
)

// CorpusEntry is one [[item]] of the corpus index.
type CorpusEntry struct {
	ID       string `toml:"id"`
	Class    string `toml:"class"`
	Rarity   string `toml:"rarity"`
	Advanced bool   `toml:"advanced"`
	// Source is where the text came from.
	Source string `toml:"source"`
}

// CorpusItem is an entry with its clipboard text.
type CorpusItem struct {
	Entry CorpusEntry
	Text  string
}

// CorpusDir is the corpus directory, found from this file's location so tests in any package
// (and tools run from any directory) see the same path.
func CorpusDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("coretest: cannot locate source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "testdata", "corpus")
}

// LoadCorpus returns every corpus entry with its text, in index order.
func LoadCorpus() ([]CorpusItem, error) {
	raw, err := os.ReadFile(filepath.Join(CorpusDir(), "index.toml"))
	if err != nil {
		return nil, fmt.Errorf("corpus index: %w", err)
	}
	var index struct {
		Item []CorpusEntry `toml:"item"`
	}
	if err := toml.Unmarshal(raw, &index); err != nil {
		return nil, fmt.Errorf("invalid corpus index: %w", err)
	}
	items := make([]CorpusItem, 0, len(index.Item))
	for _, e := range index.Item {
		text, err := CorpusText(e.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, CorpusItem{Entry: e, Text: text})
	}
	return items, nil
}

// CorpusText returns the clipboard text of one entry by id.
func CorpusText(id string) (string, error) {
	b, err := os.ReadFile(filepath.Join(CorpusDir(), id+".txt"))
	if err != nil {
		return "", fmt.Errorf("corpus item text: %w", err)
	}
	return string(b), nil
}
