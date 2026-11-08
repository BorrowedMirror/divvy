package core

// Parses every corpus item and compares the result with its golden; compares trade queries
// for a subset.

import (
	"encoding/json/v2"
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
	default:
		return string(kind)
	}
}

type corpusKey struct{ kind, template string }

// corpusCatalog knows every template seen in the corpus, with stable synthetic ids.
type corpusCatalog struct {
	stats map[corpusKey]*StatDef
}

func fnv(s string) uint32 {
	h := uint32(0x811c_9dc5)
	for i := range len(s) {
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

func TestCorpusIsLargeAndCoversAdvancedAndCRLF(t *testing.T) {
	items := loadCorpus(t)
	assert.GreaterOrEqual(t, len(items), 60, "corpus has %d items", len(items))
	advanced, crlf := 0, false
	for _, it := range items {
		if it.Entry.Advanced {
			advanced++
		}
		crlf = crlf || strings.Contains(it.Text, "\r\n")
	}
	assert.GreaterOrEqual(t, advanced, 10)
	assert.True(t, crlf)
}

func TestCorpusParsesAndMatchesIndex(t *testing.T) {
	for _, it := range loadCorpus(t) {
		e := it.Entry
		t.Run(e.ID, func(t *testing.T) {
			item, err := ParseItem(it.Text)
			require.NoError(t, err)
			assert.Equal(t, e.Class, string(item.Class.Kind), "class")
			assert.Equal(t, e.Rarity, string(item.Rarity), "rarity")
			assert.Equal(t, e.Advanced, item.Advanced, "advanced")

			// The golden leaves the raw text out.
			want := golden(t, "items", e.ID)
			redacted := *item
			redacted.Raw = "[raw]"
			assert.JSONEq(t, want, marshal(t, redacted), "golden")

			// The golden decodes into the Go types and encodes to the same value.
			var fromGolden ParsedItem
			require.NoError(t, json.Unmarshal([]byte(want), &fromGolden))
			assert.JSONEq(t, want, marshal(t, fromGolden), "golden decode/encode")

			// The Go JSON round-trips.
			var back ParsedItem
			require.NoError(t, json.Unmarshal([]byte(marshal(t, item)), &back))
			assert.Equal(t, item, &back, "round-trip")
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
		parsed[i] = mustParse(t, it.Text)
	}
	catalog := newCorpusCatalog(parsed)
	o := &QueryOptions{League: "Standard", Status: StatusOnline, WidenPct: 10, EnableAllStats: false}
	for _, id := range representativeIDs {
		t.Run(id, func(t *testing.T) {
			idx := -1
			for i, it := range all {
				if it.Entry.ID == id {
					idx = i
				}
			}
			require.GreaterOrEqual(t, idx, 0, "missing %s", id)
			item := cloneItem(parsed[idx])
			MatchStats(item, catalog)
			filters := BuildFilters(item, o)
			query := filters.ToTradeQuery()
			got := marshal(t, query)
			assert.JSONEq(t, golden(t, "queries", id), got)

			// TradeQuery and FilterSet JSON round-trip.
			var q2 TradeQuery
			require.NoError(t, json.Unmarshal([]byte(got), &q2))
			assert.Equal(t, query, q2, "query round-trip")
			var f2 FilterSet
			require.NoError(t, json.Unmarshal([]byte(marshal(t, filters)), &f2))
			assert.Equal(t, filters, f2, "filter set round-trip")
		})
	}
}
