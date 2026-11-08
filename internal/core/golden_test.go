package core

// Test inputs and expectations on disk: the shared item corpus
// (testdata/corpus, read through coretest) and the JSON goldens under testdata/ (`items/` holds the
// parsed corpus items, `queries/` the trade queries of representative items).

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core/coretest"
)

// loadCorpus returns every corpus entry with its text, in index order.
func loadCorpus(tb testing.TB) []coretest.CorpusItem {
	tb.Helper()
	items, err := coretest.LoadCorpus()
	require.NoError(tb, err)
	return items
}

// golden returns testdata/<dir>/<id>.json, with the `/` of a corpus id written as `__`.
func golden(tb testing.TB, dir, id string) string {
	tb.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", dir, strings.ReplaceAll(id, "/", "__")+".json"))
	require.NoError(tb, err)
	return string(b)
}

// marshal encodes v as JSON.
func marshal(tb testing.TB, v any) string {
	tb.Helper()
	b, err := json.Marshal(v)
	require.NoError(tb, err)
	return string(b)
}

// toValue is v's JSON decoded into generic values (map[string]any, []any, float64, ...).
func toValue(tb testing.TB, v any) any {
	tb.Helper()
	var out any
	require.NoError(tb, json.Unmarshal([]byte(marshal(tb, v)), &out))
	return out
}

// cloneItem copies an item deeply enough for [MatchStats], which rewrites names, stat ids
// and modifier values.
func cloneItem(p *ParsedItem) *ParsedItem {
	out := *p
	out.Properties = slices.Clone(p.Properties)
	out.Modifiers = slices.Clone(p.Modifiers)
	for i := range out.Modifiers {
		out.Modifiers[i].Values = slices.Clone(p.Modifiers[i].Values)
	}
	return &out
}

// anyRune draws digits and punctuation the parser cares about often, any rune otherwise.
func anyRune() *rapid.Generator[rune] {
	return rapid.OneOf(rapid.RuneFrom([]rune("0123456789,.:+-%#(){}\" \r\n٣")), rapid.Rune())
}

func TestGoldenHelpers(t *testing.T) {
	assert.Equal(t, map[string]any{"a": 1.0}, toValue(t, map[string]int{"a": 1}))
	item := &ParsedItem{Modifiers: []Modifier{{Values: []float64{1}}}}
	c := cloneItem(item)
	c.Modifiers[0].Values[0] = 2
	assert.InDelta(t, 1.0, item.Modifiers[0].Values[0], 0)
}
