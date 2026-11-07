package testkit

// Corpus format
//
// crates/testkit/corpus/index.toml lists entries:
//
//	[[item]]
//	id = "currency/divine-orb"         # path under corpus/ without extension
//	class = "StackableCurrency"        # expected ItemClass variant name
//	rarity = "currency"                # expected Rarity (snake_case)
//	advanced = false                   # copied with Ctrl+Alt+C
//	source = "hand-written from 0.3 in-game copy"
//
// Each entry has corpus/<id>.txt containing the exact clipboard text (LF line endings; the
// parser must also accept CRLF). Expected parse output is a snapshot next to the core tests.

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
	// Source is where the text came from; nil when the entry has none.
	Source *string `toml:"source"`
}

// CorpusItem is an entry with its clipboard text.
type CorpusItem struct {
	Entry CorpusEntry
	Text  string
}

// repoRoot is the repository root, found from this file's location so tests in any package (and
// any working directory) see the same paths.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("testkit: cannot locate source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// CorpusDir is crates/testkit/corpus.
func CorpusDir() string {
	return filepath.Join(repoRoot(), "crates", "testkit", "corpus")
}

// LoadCorpus returns all corpus entries with their text, in index order.
func LoadCorpus() ([]CorpusItem, error) {
	dir := CorpusDir()
	raw, err := os.ReadFile(filepath.Join(dir, "index.toml"))
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
