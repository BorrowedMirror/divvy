package core

// Port of crates/core/tests/corpus.rs: parses every corpus item and compares the result with
// the Rust insta snapshots; compares trade queries for a subset. Snapshots are read, never
// written.

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kindName(kind ModKind) string {
	switch kind {
	case ModExplicit, ModSanctified:
		return "explicit"
	}
	return string(kind)
}

type corpusKey struct{ kind, template string }

// corpusCatalog knows every template seen in the corpus, with stable synthetic ids.
type corpusCatalog struct {
	NoUniquesByBase
	stats map[corpusKey]*StatDef
}

func fnv(s string) uint32 {
	h := uint32(0x811c_9dc5)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 0x0100_0193
	}
	return h
}

func newCorpusCatalog(items []*ParsedItem) *corpusCatalog {
	stats := map[corpusKey]*StatDef{}
	for _, it := range items {
		for _, m := range it.Modifiers {
			kind := kindName(m.Kind)
			id := kind + ".stat_" + strconv.FormatUint(uint64(fnv(m.Template)), 10)
			stats[corpusKey{kind, m.Template}] = &StatDef{ID: id, Text: m.Template, Kind: kind, Aliases: []StatAlias{}}
		}
	}
	return &corpusCatalog{stats: stats}
}

func (c *corpusCatalog) StatsByTemplate(template string, kind ModKind) []StatMatch {
	if s, ok := c.stats[corpusKey{kindName(kind), template}]; ok {
		return []StatMatch{{Stat: s}}
	}
	return nil
}
func (c *corpusCatalog) Base(string) *BaseDef            { return nil }
func (c *corpusCatalog) BaseInMagicName(string) *BaseDef { return nil }
func (c *corpusCatalog) Unique(string) *UniqueDef        { return nil }
func (c *corpusCatalog) StaticItem(string) *StaticDef    { return nil }

func variantName(item *ParsedItem) string { return item.Class.Kind.VariantName() }

func TestCorpusIsLargeAndCoversAdvancedAndCRLF(t *testing.T) {
	items := loadCorpus(t)
	assert.GreaterOrEqual(t, len(items), 60, "corpus has %d items", len(items))
	advanced, crlf := 0, false
	for _, it := range items {
		if it.entry.Advanced {
			advanced++
		}
		crlf = crlf || strings.Contains(it.text, "\r\n")
	}
	assert.GreaterOrEqual(t, advanced, 10)
	assert.True(t, crlf)
}

// genericJSON decodes JSON into map[string]any / []any / float64 / string / bool / nil.
func genericJSON(t testing.TB, b []byte) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(b, &v))
	return v
}

func TestCorpusParsesAndMatchesIndex(t *testing.T) {
	for _, it := range loadCorpus(t) {
		e := it.entry
		t.Run(e.ID, func(t *testing.T) {
			item, err := ParseItem(it.text)
			require.NoError(t, err, e.ID)
			assert.Equal(t, e.Class, variantName(item), "%s class", e.ID)
			assert.Equal(t, e.Rarity, toValue(t, item.Rarity), "%s rarity", e.ID)
			assert.Equal(t, e.Advanced, item.Advanced, "%s advanced", e.ID)

			// Snapshot, with the `.raw` redaction insta applied.
			redacted := item.Clone()
			redacted.Raw = "[raw]"
			want, err := parseInstaYAML(readSnapshot(t, "items", snapshotName(e.ID)))
			require.NoError(t, err)
			got, err := MarshalSerde(redacted)
			require.NoError(t, err)
			assert.Equal(t, want, genericJSON(t, got), "%s snapshot", e.ID)

			// The Rust output decodes into the Go types and re-encodes to the same value.
			wantJSON, err := json.Marshal(want)
			require.NoError(t, err)
			var fromRust ParsedItem
			require.NoError(t, json.Unmarshal(wantJSON, &fromRust))
			again, err := MarshalSerde(fromRust)
			require.NoError(t, err)
			assert.Equal(t, want, genericJSON(t, again), "%s decode/encode of the Rust snapshot", e.ID)

			// Go JSON round-trips byte for byte.
			full, err := MarshalSerde(item)
			require.NoError(t, err)
			var back ParsedItem
			require.NoError(t, json.Unmarshal(full, &back))
			assert.Equal(t, item, &back, "%s round-trip value", e.ID)
			full2, err := MarshalSerde(back)
			require.NoError(t, err)
			assert.Equal(t, string(full), string(full2), "%s round-trip bytes", e.ID)
		})
	}
}

var representativeIDs = []string{
	"currency/divine-orb",
	"gem/lightning-arrow-corrupted",
	"waystone/rare-tier-15",
	"jewel/rare-ruby",
	"ring/rare-sapphire-ring-advanced",
	"helmet/unique-goldrim",
	"body-armour/rare-vile-robe",
	"weapon/rare-recurve-bow",
	"weapon/unidentified-rare-crossbow",
	"gloves/magic-gloves",
}

func TestTradeQueriesForRepresentativeItems(t *testing.T) {
	all := loadCorpus(t)
	parsed := make([]*ParsedItem, len(all))
	for i, it := range all {
		parsed[i] = mustParse(t, it.text)
	}
	matcher := NewStatMatcher(newCorpusCatalog(parsed))
	o := &QueryOptions{League: "Standard", Status: StatusOnline, WidenPct: 10, EnableAllStats: false}
	for _, id := range representativeIDs {
		t.Run(id, func(t *testing.T) {
			idx := -1
			for i, it := range all {
				if it.entry.ID == id {
					idx = i
				}
			}
			require.GreaterOrEqual(t, idx, 0, "missing %s", id)
			item := parsed[idx].Clone()
			matcher.MatchItem(item)
			filters := BuildFilters(item, o)
			query := filters.ToTradeQuery()

			body := readSnapshot(t, "queries", snapshotName(id))
			got, err := MarshalSerde(query)
			require.NoError(t, err)
			assert.Equal(t, genericJSON(t, []byte(body)), genericJSON(t, got), "%s semantic", id)
			// insta pretty-prints serde_json output with two-space indents: byte-compare too,
			// which pins key order and float formatting (`37.0`).
			var pretty bytes.Buffer
			require.NoError(t, json.Indent(&pretty, got, "", "  "))
			assert.Equal(t, strings.TrimRight(body, "\n"), pretty.String(), "%s bytes", id)

			// TradeQuery and FilterSet JSON round-trip byte for byte.
			var q2 TradeQuery
			require.NoError(t, json.Unmarshal(got, &q2))
			got2, err := MarshalSerde(q2)
			require.NoError(t, err)
			assert.Equal(t, string(got), string(got2), "%s query round-trip", id)
			fb, err := MarshalSerde(filters)
			require.NoError(t, err)
			var f2 FilterSet
			require.NoError(t, json.Unmarshal(fb, &f2))
			assert.Equal(t, filters, f2, "%s filter set round-trip value", id)
			fb2, err := MarshalSerde(f2)
			require.NoError(t, err)
			assert.Equal(t, string(fb), string(fb2), "%s filter set round-trip bytes", id)
		})
	}
}
