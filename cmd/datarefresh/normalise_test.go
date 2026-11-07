package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

func alias(text string, negate bool) core.StatAlias {
	return core.StatAlias{Text: text, Negate: negate}
}

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

func TestAliasesUnsignedFlat(t *testing.T) {
	eq(t, statAliases("# to maximum Life"), []core.StatAlias{alias("+# to maximum Life", false), alias("-# to maximum Life", true)})
}

func TestAliasesSignedFlat(t *testing.T) {
	eq(t, statAliases("+#% to Fire Resistance"),
		[]core.StatAlias{alias("#% to Fire Resistance", false), alias("-#% to Fire Resistance", true)})
}

func TestAliasesIncreasedReducedMoreLess(t *testing.T) {
	eq(t, statAliases("#% increased Movement Speed"), []core.StatAlias{
		alias("+#% increased Movement Speed", false),
		alias("-#% increased Movement Speed", true),
		alias("#% reduced Movement Speed", true),
	})
	eq(t, statAliases("Players gain #% reduced Flask Charges"),
		[]core.StatAlias{alias("Players gain #% increased Flask Charges", true)})
	eq(t, statAliases("Deal #% more Damage"), []core.StatAlias{alias("Deal #% less Damage", true)})
	eq(t, statAliases("Take #% less Damage"), []core.StatAlias{alias("Take #% more Damage", true)})
}

func TestAliasesNoneForPlainText(t *testing.T) {
	if got := statAliases("Dazes on Hit"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

const statsJSON = `{"result":[
	{"id":"explicit","label":"Explicit","entries":[
		{"id":"explicit.stat_2","text":"#% reduced Mana Cost of Skills","type":"explicit"},
		{"id":"explicit.stat_1","text":"#% increased Mana Cost of Skills","type":"explicit"},
		{"id":"explicit.stat_1","text":"#% increased Mana Cost of Skills","type":"explicit"}
	]},
	{"id":"implicit","label":"Implicit","entries":[
		{"id":"implicit.stat_3","text":"# to Spirit","type":"implicit"}
	]}
]}`

func TestStatsSortedDedupedAndCollidingAliasesDropped(t *testing.T) {
	s, err := normaliseStats([]byte(statsJSON))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range s {
		ids = append(ids, d.ID)
	}
	eq(t, ids, []string{"explicit.stat_1", "explicit.stat_2", "implicit.stat_3"})
	eq(t, s[0].Kind, "explicit")
	// `#% reduced Mana Cost of Skills` is a real stat, so not an alias of stat_1.
	for _, a := range s[0].Aliases {
		if a.Text == "#% reduced Mana Cost of Skills" {
			t.Errorf("colliding alias kept: %v", a)
		}
	}
	eq(t, len(s[2].Aliases), 2)
}

func TestWordingVariantsStripSuffixAndMapWording(t *testing.T) {
	eq(t, wordingVariants("#% increased Armour (Local)"), []string{"#% increased Armour"})
	eq(t, wordingVariants("#% increased Duration (Charm)"), []string{"#% increased Duration"})
	eq(t, wordingVariants("#% increased Gold found in Map (Gold Piles)"), []string{
		"#% increased Gold found in Map",
		"#% increased Gold found in your Maps (Gold Piles)",
		"#% increased Gold found in your Maps",
	})
	eq(t, wordingVariants("#% increased Quantity of Items found in Map"),
		[]string{"#% increased Quantity of Items found in your Maps"})
	eq(t, wordingVariants("Breaches in Map have # Monsters"), []string{"Breaches in your Maps have # Monsters"})
	// Not a whole word / not a label suffix.
	for _, s := range []string{"Delirium in Maps", "# to Spirit (per socket)", "Has ()", "Dazes on Hit"} {
		if got := wordingVariants(s); len(got) != 0 {
			t.Errorf("wordingVariants(%q) = %q", s, got)
		}
	}
	eq(t, wordingVariants("#% increased Attack Speed per 10 Dexterity"), []string{"#% increased Attack Speed per # Dexterity"})
	eq(t, wordingVariants("Grants Skill: Level # Parry"), []string{"Grants Skill: Parry"})
}

func TestStatsKeepSameIDWordingsAndLocalVariants(t *testing.T) {
	raw := `{"result":[{"id":"explicit","entries":[
		{"id":"explicit.xp","text":"#% increased Experience gain"},
		{"id":"explicit.xp","text":"#% increased Experience gain in Map"},
		{"id":"explicit.armour_local","text":"#% increased Armour (Local)"},
		{"id":"explicit.armour","text":"#% increased Armour"}
	]}]}`
	s, err := normaliseStats([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	byID := func(id string) core.StatDef {
		i := slices.IndexFunc(s, func(d core.StatDef) bool { return d.ID == id })
		if i < 0 {
			t.Fatalf("no stat %s", id)
		}
		return s[i]
	}
	has := func(id, text string, negate bool) {
		t.Helper()
		if !slices.Contains(byID(id).Aliases, alias(text, negate)) {
			t.Errorf("%s lacks alias %q (negate %v)", id, text, negate)
		}
	}
	eq(t, byID("explicit.xp").Text, "#% increased Experience gain")
	has("explicit.xp", "#% increased Experience gain in Map", false)
	has("explicit.xp", "#% increased Experience gain in your Maps", false)
	has("explicit.xp", "#% reduced Experience gain in your Maps", true)
	// Local variant keeps the unsuffixed wording even though a global stat has it.
	has("explicit.armour_local", "#% increased Armour", false)
	has("explicit.armour_local", "#% reduced Armour", true)
}

func TestStatsRejectsGarbage(t *testing.T) {
	for _, raw := range []string{`{}`, `{"result":null}`, `{"result":[{"id":"x"}]}`, `{"result":[{"id":"x","entries":[{"id":"a"}]}]}`, `[`} {
		if _, err := normaliseStats([]byte(raw)); err == nil {
			t.Errorf("normaliseStats(%s) succeeded", raw)
		}
	}
}

const itemsJSON = `{"result":[
	{"id":"armour","label":"Armour","entries":[
		{"type":"Hunter Hood"},
		{"type":"Hunter Hood","text":"Elevore Hunter Hood","name":"Elevore","flags":{"unique":true}},
		{"type":"Furtive Wraps","text":"Hand Furtive Wraps (Legacy)","name":"Hand","disc":"legacy","flags":{"unique":true}},
		{"type":"Mystery Plate"},
		{"type":"Hunter Hood"}
	]},
	{"id":"accessory","label":"Accessories","entries":[
		{"type":"Gold Ring"},
		{"type":"Gold Ring","text":"Andvarius Gold Ring","name":"Andvarius","flags":{"unique":true}}
	]}
]}`

func repoeSamples(t *testing.T) (map[string]repoeBase, map[string]repoeItemClass) {
	t.Helper()
	var bases map[string]repoeBase
	err := json.Unmarshal([]byte(`{
		"Metadata/Items/Armours/Helmets/FourHelmetDex5": {"name":"Hunter Hood","item_class":"Helmet","release_state":"released"},
		"Metadata/Items/Hidden/HunterHood": {"name":"Hunter Hood","item_class":"HiddenItem","release_state":"unreleased"},
		"Metadata/Items/Rings/Ring1": {"name":"Gold Ring","item_class":"Ring","release_state":"released"}
	}`), &bases)
	if err != nil {
		t.Fatal(err)
	}
	var classes map[string]repoeItemClass
	if err := json.Unmarshal([]byte(`{"Helmet":{"name":"Helmets"},"Ring":{"name":"Rings"},"HiddenItem":{}}`), &classes); err != nil {
		t.Fatal(err)
	}
	return bases, classes
}

func TestBasesEnrichedFromRepoe(t *testing.T) {
	b, c := repoeSamples(t)
	out, err := normaliseBases([]byte(itemsJSON), b, c)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, out, []core.BaseDef{
		{Name: "Gold Ring", Category: "accessory.ring", ItemClass: "Rings"},
		{Name: "Hunter Hood", Category: "armour.helmet", ItemClass: "Helmets"},
		{Name: "Mystery Plate", Category: "armour", ItemClass: "Armour"},
	})
}

func TestRepoeBaseRequiresFields(t *testing.T) {
	var bases map[string]repoeBase
	if err := json.Unmarshal([]byte(`{"x":{"name":"Hunter Hood"}}`), &bases); err == nil {
		t.Error("a RePoE base without item_class decoded")
	}
}

func TestUniquesSkipLegacy(t *testing.T) {
	out, err := normaliseUniques([]byte(itemsJSON))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, out, []core.UniqueDef{{Name: "Andvarius", BaseType: "Gold Ring"}, {Name: "Elevore", BaseType: "Hunter Hood"}})
}

func TestStaticsSkipSeparatorsAndAbsolutiseImages(t *testing.T) {
	raw := `{"result":[
		{"id":"Currency","label":"Currency","entries":[
			{"id":"divine","text":"Divine Orb","image":"/gen/image/divine.png"},
			{"id":"sep","text":""},
			{"id":"chaos","text":"Chaos Orb","image":"https://example.invalid/chaos.png"}
		]},
		{"id":"UncutGems","label":"Uncut Gems","entries":[{"id":"uncut-1","text":"Uncut Skill Gem (Level 1)"}]}
	]}`
	out, err := normaliseStatics([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(out), 3)
	eq(t, out[0].ID, "chaos")
	eq(t, *out[0].Image, "https://example.invalid/chaos.png")
	eq(t, *out[1].Image, "https://web.poecdn.com/gen/image/divine.png")
	eq(t, out[2].Group, "UncutGems")
	if out[2].Image != nil {
		t.Errorf("image = %q", *out[2].Image)
	}
}

func TestGameVersionFromIndex(t *testing.T) {
	for in, want := range map[string]string{
		"<title>RePoE - PoE2 version 0.5.5.5</title>": "0.5.5.5",
		"PoE2 version 1.0.":                           "1.0",
		"nothing here":                                "",
		"PoE2 version x":                              "",
	} {
		got := ""
		if v := gameVersion(in); v != nil {
			got = *v
		}
		if got != want {
			t.Errorf("gameVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCategories(t *testing.T) {
	eq(t, tradeCategories["Warstaff"], "weapon.warstaff")
	if _, ok := tradeCategories["QuestItem"]; ok {
		t.Error("QuestItem has a category")
	}
}
