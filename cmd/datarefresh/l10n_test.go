package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/data"
)

func TestMarkupAndShapes(t *testing.T) {
	assert.Equal(t, "Evasion Rating", stripMarkup("[Evasion|Evasion Rating]"))
	assert.Equal(t, "Armour and Critical Hit Chance", stripMarkup("[Armour] and [Critical|Critical Hit] Chance"))
	assert.Equal(t, shape("+# to maximum Life"), shape("{0:+d} to maximum Life"))
	assert.Equal(t, "#% increased Damage per # Dexterity", shape("{0}% increased Damage per 10 Dexterity"))
}

const csd = "description\n\t1 base_maximum_life\n\t1\n\t\t# \"{0:+d} to maximum Life\"\n\tlang \"German\"\n\t1\n\t\t# \"{0:+d} zu maximalem Leben\"\n\n" +
	"description\n\t1 armour\n\t2\n\t\t1|# \"{0}% increased [Armour]\"\n\t\t#|-1 \"{0}% reduced [Armour]\" negate 1\n\tlang \"German\"\n\t2\n\t\t1|# \"{0}% erhöhte [Armour|Rüstung]\"\n\t\t#|-1 \"{0}% verringerte [Armour|Rüstung]\" negate 1\n"

func TestParsesDescriptionsWithLanguages(t *testing.T) {
	descs := descriptions(csd)
	assert.Len(t, descs, 2)
	searchable := map[string]bool{}
	for _, s := range []string{"# to maximum Life", "#% increased Armour", "#% reduced Armour"} {
		searchable[shape(s)] = true
	}
	assert.Equal(t, [][2]string{
		{"{0:+d} to maximum Life", "{0:+d} zu maximalem Leben"},
		{"{0}% increased Armour", "{0}% erhöhte Rüstung"},
		{"{0}% reduced Armour", "{0}% verringerte Rüstung"},
	}, statPairs(descs, "German", searchable))
	// A byte order mark and CRLF line ends parse the same.
	assert.Len(t, descriptions("\ufeff"+strings.ReplaceAll(csd, "\n", "\r\n")), 2)
}

func TestQuotedTextUnescapes(t *testing.T) {
	got, ok := quoted(`1|# "Prefix \"{0}\"" negate 1`)
	assert.Equal(t, `Prefix "{0}"`, got)
	assert.True(t, ok)
	_, ok = quoted("no quotes")
	assert.False(t, ok)
}

func TestParseCount(t *testing.T) {
	for in, want := range map[string]int{"0": 0, "12": 12, "+3": 3} {
		got, ok := parseCount(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "+", "-1", "++1", "1_0", " 1", "x"} {
		_, ok := parseCount(in)
		assert.False(t, ok, in)
	}
}

// rows builds two-column table rows; "-" stands for a column that points nowhere.
func rows(pairs ...string) [][]*string {
	var out [][]*string
	for pair := range slices.Chunk(pairs, 2) {
		row := make([]*string, 2)
		for j, s := range pair {
			if s != "-" {
				row[j] = new(s)
			}
		}
		out = append(out, row)
	}
	return out
}

func TestPairRows(t *testing.T) {
	en := rows("a", "Hunter Hood", "b", "Gold Ring", "c", "", "d", "Hunter Hood")
	local := rows("a", "[X|Jägerhaube]", "b", "Goldring", "c", "Leer", "d", "Jägerhaube")
	got, err := pairRows(en, local, 0, 1, 1, func(n string) bool { return n != "Gold Ring" })
	require.NoError(t, err)
	assert.Equal(t, [][2]string{{"Hunter Hood", "Jägerhaube"}}, got)
	_, err = pairRows(en, local[:1], 0, 1, 1, func(string) bool { return true })
	require.Error(t, err, "row count mismatch accepted")
	_, err = pairRows(rows("a", "x"), rows("-", "y"), 0, 1, 1, func(string) bool { return true })
	require.Error(t, err, "id mismatch accepted")
	// Without an id column the ids are not compared.
	got, err = pairRows(rows("a", "x"), rows("-", "y"), -1, 1, 1, func(string) bool { return true })
	require.NoError(t, err)
	assert.Equal(t, [][2]string{{"x", "y"}}, got)
}

func TestPickChecksEnglishAndDedupes(t *testing.T) {
	en := map[string]string{"ItemDisplayStringClass": "Item Class: ", "ItemDisplayStringRarity": "Rarity:", "Renamed": "Other"}
	local := map[string]string{"ItemDisplayStringClass": "[Klasse|Gegenstandsklasse]：", "ItemDisplayStringRarity": "Seltenheit:", "Renamed": "x"}
	ids := []clientKey{
		{"ItemDisplayStringClass", "Item Class"}, {"ItemDisplayStringClass", "Item Class"},
		{"ItemDisplayStringRarity", "Rarity"}, {"Renamed", "Expected"}, {"Missing", "Missing"},
	}
	assert.Equal(t, [][2]string{{"Item Class", "Gegenstandsklasse"}, {"Rarity", "Seltenheit"}}, pick(en, local, ids, true, "German"))
}

// gameTables returns one folder's tables for the l10n test.
func gameTables(classStr, rarity, unidentified, class, base, unique string) map[string][][]*string {
	return map[string][][]*string{
		"clientstrings": rows(
			"ItemDisplayStringClass", classStr,
			"ItemDisplayStringRarity", rarity,
			"ItemPopupUnidentified", unidentified,
		),
		"clientstrings2": rows("ItemTierDisplay", " (T{0})", "ItemDisplayStringRarity", "ignored"),
		"itemclasses":    rows("Helmet", class),
		"baseitemtypes":  rows("Metadata/Hood", base, "Metadata/Other", base+" 2"),
		"words":          rows(unique, "-", "Elevore", unique),
	}
}

func TestL10nBuildsEveryLanguage(t *testing.T) {
	assets := t.TempDir()
	bundle := data.Bundle{
		Stats:   []core.StatDef{{ID: "explicit.life", Text: "# to maximum Life", Kind: "explicit"}},
		Bases:   []core.BaseDef{{Name: "Hunter Hood", Category: "armour.helmet", ItemClass: "Helmets"}},
		Uniques: []core.UniqueDef{{Name: "Elevore", BaseType: "Hunter Hood"}},
	}
	sources := data.AssetSources{Stats: []string{"stats-url"}}
	_, err := data.WriteDir(assets, &bundle, &sources, "2026-01-01T00:00:00Z", new("0.5"))
	require.NoError(t, err)
	raw := &rawGame{Folders: map[string]map[string][][]*string{
		"": gameTables("Item Class:", "Rarity", "Unidentified", "Helmets", "Hunter Hood", "Elevore"),
	}}
	raw.StatDescriptions = append(raw.StatDescriptions, statFile{"data/statdescriptions/stat_descriptions.csd", csd})
	for _, l := range languages {
		raw.Folders[l.folder] = gameTables("Klasse:", "Seltenheit", "Nicht identifiziert", "Helme", "Jägerhaube", "Elevore-"+l.code)
	}
	require.NoError(t, l10n(raw, assets))
	got, err := data.LoadDir(assets)
	require.NoError(t, err)
	m := got.Manifest
	assert.Equal(t, "2026-01-01T00:00:00Z", m.GeneratedAt)
	assert.Equal(t, "0.5", *m.GameVersion)
	assert.Equal(t, []string{"stats-url"}, m.Stats.Sources)
	assert.Equal(t, []string{l10nSource}, m.L10n.Sources)
	langs := got.Bundle().Languages
	assert.Len(t, langs, len(languages))
	de := langs[0]
	assert.Equal(t, "German", de.Name)
	// clientstrings2 repeats the rarity key: the first table wins.
	assert.Equal(t, [][2]string{{"Item Class", "Klasse"}, {"Rarity", "Seltenheit"}}, de.Keys)
	assert.Equal(t, [][2]string{}, de.Rarities)
	assert.Equal(t, [][2]string{{"Unidentified", "Nicht identifiziert"}}, de.Flags)
	assert.Equal(t, [][2]string{{"Unidentified (Tier {0})", "Nicht identifiziert (T{0})"}}, de.Patterns)
	assert.Equal(t, [][2]string{{"Helmets", "Helme"}}, de.Classes)
	assert.Equal(t, [][2]string{{"Hunter Hood", "Jägerhaube"}}, de.Bases)
	assert.Equal(t, [][2]string{{"Elevore", "Elevore-de"}}, de.Uniques)
	assert.Equal(t, [][2]string{{"{0:+d} to maximum Life", "{0:+d} zu maximalem Leben"}}, de.Stats)
	assert.Equal(t, "ko", langs[7].Code)
	assert.Empty(t, langs[7].Stats)
}

func TestL10nNeedsEveryFolder(t *testing.T) {
	assets := t.TempDir()
	_, err := data.WriteDir(assets, &data.Bundle{}, &data.AssetSources{}, "now", nil)
	require.NoError(t, err)
	raw := &rawGame{Folders: map[string]map[string][][]*string{
		"": gameTables("Item Class", "Rarity", "Unidentified", "Helmets", "Hunter Hood", "Elevore"),
	}}
	raw.StatDescriptions = append(raw.StatDescriptions, statFile{"x.csd", csd})
	assert.ErrorContains(t, l10n(raw, assets), `"german"`)
}
