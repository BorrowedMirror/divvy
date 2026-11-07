package main

import (
	"strings"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
)

func TestMarkupAndShapes(t *testing.T) {
	eq(t, stripMarkup("[Evasion|Evasion Rating]"), "Evasion Rating")
	eq(t, stripMarkup("[Armour] and [Critical|Critical Hit] Chance"), "Armour and Critical Hit Chance")
	eq(t, shape("{0:+d} to maximum Life"), shape("+# to maximum Life"))
	eq(t, shape("{0}% increased Damage per 10 Dexterity"), "#% increased Damage per # Dexterity")
}

const csd = "description\n\t1 base_maximum_life\n\t1\n\t\t# \"{0:+d} to maximum Life\"\n\tlang \"German\"\n\t1\n\t\t# \"{0:+d} zu maximalem Leben\"\n\n" +
	"description\n\t1 armour\n\t2\n\t\t1|# \"{0}% increased [Armour]\"\n\t\t#|-1 \"{0}% reduced [Armour]\" negate 1\n\tlang \"German\"\n\t2\n\t\t1|# \"{0}% erhöhte [Armour|Rüstung]\"\n\t\t#|-1 \"{0}% verringerte [Armour|Rüstung]\" negate 1\n"

func TestParsesDescriptionsWithLanguages(t *testing.T) {
	descs := descriptions(csd)
	eq(t, len(descs), 2)
	searchable := map[string]bool{}
	for _, s := range []string{"# to maximum Life", "#% increased Armour", "#% reduced Armour"} {
		searchable[shape(s)] = true
	}
	eq(t, statPairs(descs, "German", searchable), [][2]string{
		{"{0:+d} to maximum Life", "{0:+d} zu maximalem Leben"},
		{"{0}% increased Armour", "{0}% erhöhte Rüstung"},
		{"{0}% reduced Armour", "{0}% verringerte Rüstung"},
	})
	// A byte order mark and CRLF line ends parse the same.
	eq(t, len(descriptions("\ufeff"+strings.ReplaceAll(csd, "\n", "\r\n"))), 2)
}

func TestQuotedTextUnescapes(t *testing.T) {
	got, ok := quoted(`1|# "Prefix \"{0}\"" negate 1`)
	eq(t, got, `Prefix "{0}"`)
	eq(t, ok, true)
	_, ok = quoted("no quotes")
	eq(t, ok, false)
}

func TestParseCountLikeRust(t *testing.T) {
	for in, want := range map[string]int{"0": 0, "12": 12, "+3": 3} {
		got, ok := parseCount(in)
		if !ok || got != want {
			t.Errorf("parseCount(%q) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "+", "-1", "++1", "1_0", " 1", "x"} {
		if _, ok := parseCount(in); ok {
			t.Errorf("parseCount(%q) succeeded", in)
		}
	}
}

func str(s string) *string { return &s }

// rows builds two-column table rows; "-" stands for a column that points nowhere.
func rows(pairs ...string) [][]*string {
	var out [][]*string
	for i := 0; i < len(pairs); i += 2 {
		row := make([]*string, 2)
		for j := range 2 {
			if pairs[i+j] != "-" {
				row[j] = str(pairs[i+j])
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
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, [][2]string{{"Hunter Hood", "Jägerhaube"}})
	if _, err := pairRows(en, local[:1], 0, 1, 1, func(string) bool { return true }); err == nil {
		t.Error("row count mismatch accepted")
	}
	if _, err := pairRows(rows("a", "x"), rows("-", "y"), 0, 1, 1, func(string) bool { return true }); err == nil {
		t.Error("id mismatch accepted")
	}
	// Without an id column the ids are not compared.
	got, err = pairRows(rows("a", "x"), rows("-", "y"), -1, 1, 1, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, [][2]string{{"x", "y"}})
}

func TestPickChecksEnglishAndDedupes(t *testing.T) {
	en := map[string]string{"ItemDisplayStringClass": "Item Class: ", "ItemDisplayStringRarity": "Rarity:", "Renamed": "Other"}
	local := map[string]string{"ItemDisplayStringClass": "[Klasse|Gegenstandsklasse]：", "ItemDisplayStringRarity": "Seltenheit:", "Renamed": "x"}
	ids := []clientKey{
		{"ItemDisplayStringClass", "Item Class"}, {"ItemDisplayStringClass", "Item Class"},
		{"ItemDisplayStringRarity", "Rarity"}, {"Renamed", "Expected"}, {"Missing", "Missing"},
	}
	eq(t, pick(en, local, ids, true, "German"), [][2]string{{"Item Class", "Gegenstandsklasse"}, {"Rarity", "Seltenheit"}})
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
	if _, err := data.WriteDir(assets, &bundle, &sources, "2026-01-01T00:00:00Z", str("0.5")); err != nil {
		t.Fatal(err)
	}
	raw := &rawGame{Folders: map[string]map[string][][]*string{
		"": gameTables("Item Class:", "Rarity", "Unidentified", "Helmets", "Hunter Hood", "Elevore"),
	}}
	raw.StatDescriptions = append(raw.StatDescriptions, statFile{"data/statdescriptions/stat_descriptions.csd", csd})
	for _, l := range languages {
		raw.Folders[l.folder] = gameTables("Klasse:", "Seltenheit", "Nicht identifiziert", "Helme", "Jägerhaube", "Elevore-"+l.code)
	}
	if err := l10n(raw, assets); err != nil {
		t.Fatal(err)
	}
	got, err := data.LoadDir(assets)
	if err != nil {
		t.Fatal(err)
	}
	m := got.Manifest
	eq(t, m.GeneratedAt, "2026-01-01T00:00:00Z")
	eq(t, *m.GameVersion, "0.5")
	eq(t, m.Stats.Sources, []string{"stats-url"})
	eq(t, m.L10n.Sources, []string{l10nSource})
	langs := got.Bundle().Languages
	eq(t, len(langs), len(languages))
	de := langs[0]
	eq(t, de.Name, "German")
	// clientstrings2 repeats the rarity key: the first table wins.
	eq(t, de.Keys, [][2]string{{"Item Class", "Klasse"}, {"Rarity", "Seltenheit"}})
	eq(t, de.Rarities, [][2]string{})
	eq(t, de.Flags, [][2]string{{"Unidentified", "Nicht identifiziert"}})
	eq(t, de.Patterns, [][2]string{{"Unidentified (Tier {0})", "Nicht identifiziert (T{0})"}})
	eq(t, de.Classes, [][2]string{{"Helmets", "Helme"}})
	eq(t, de.Bases, [][2]string{{"Hunter Hood", "Jägerhaube"}})
	eq(t, de.Uniques, [][2]string{{"Elevore", "Elevore-de"}})
	eq(t, de.Stats, [][2]string{{"{0:+d} to maximum Life", "{0:+d} zu maximalem Leben"}})
	eq(t, langs[7].Code, "ko")
	eq(t, len(langs[7].Stats), 0)
}

func TestL10nNeedsEveryFolder(t *testing.T) {
	assets := t.TempDir()
	if _, err := data.WriteDir(assets, &data.Bundle{}, &data.AssetSources{}, "now", nil); err != nil {
		t.Fatal(err)
	}
	raw := &rawGame{Folders: map[string]map[string][][]*string{
		"": gameTables("Item Class", "Rarity", "Unidentified", "Helmets", "Hunter Hood", "Elevore"),
	}}
	raw.StatDescriptions = append(raw.StatDescriptions, statFile{"x.csd", csd})
	err := l10n(raw, assets)
	if err == nil || !strings.Contains(err.Error(), `"german"`) {
		t.Fatalf("err = %v", err)
	}
}
