package main

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/core"
)

func alias(text string, negate bool) core.StatAlias {
	return core.StatAlias{Text: text, Negate: negate}
}

func TestAliasesUnsignedFlat(t *testing.T) {
	assert.Equal(t, []core.StatAlias{alias("+# to maximum Life", false), alias("-# to maximum Life", true)}, statAliases("# to maximum Life"))
}

func TestAliasesSignedFlat(t *testing.T) {
	assert.Equal(t, []core.StatAlias{alias("#% to Fire Resistance", false), alias("-#% to Fire Resistance", true)}, statAliases("+#% to Fire Resistance"))
}

func TestAliasesIncreasedReducedMoreLess(t *testing.T) {
	assert.Equal(t, []core.StatAlias{
		alias("+#% increased Movement Speed", false),
		alias("-#% increased Movement Speed", true),
		alias("#% reduced Movement Speed", true),
	}, statAliases("#% increased Movement Speed"))
	assert.Equal(t, []core.StatAlias{alias("Players gain #% increased Flask Charges", true)}, statAliases("Players gain #% reduced Flask Charges"))
	assert.Equal(t, []core.StatAlias{alias("Deal #% less Damage", true)}, statAliases("Deal #% more Damage"))
	assert.Equal(t, []core.StatAlias{alias("Take #% more Damage", true)}, statAliases("Take #% less Damage"))
}

func TestAliasesNoneForPlainText(t *testing.T) {
	assert.Empty(t, statAliases("Dazes on Hit"))
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
	require.NoError(t, err)
	var ids []string
	for _, d := range s {
		ids = append(ids, d.ID)
	}
	assert.Equal(t, []string{"explicit.stat_1", "explicit.stat_2", "implicit.stat_3"}, ids)
	assert.Equal(t, "explicit", s[0].Kind)
	// `#% reduced Mana Cost of Skills` is a real stat, so not an alias of stat_1.
	for _, a := range s[0].Aliases {
		assert.NotEqual(t, "#% reduced Mana Cost of Skills", a.Text, "colliding alias kept")
	}
	assert.Len(t, s[2].Aliases, 2)
}

func TestWordingVariantsStripSuffixAndMapWording(t *testing.T) {
	assert.Equal(t, []string{"#% increased Armour"}, wordingVariants("#% increased Armour (Local)"))
	assert.Equal(t, []string{"#% increased Duration"}, wordingVariants("#% increased Duration (Charm)"))
	assert.Equal(t, []string{
		"#% increased Gold found in Map",
		"#% increased Gold found in your Maps (Gold Piles)",
		"#% increased Gold found in your Maps",
	}, wordingVariants("#% increased Gold found in Map (Gold Piles)"))
	assert.Equal(t, []string{"#% increased Quantity of Items found in your Maps"}, wordingVariants("#% increased Quantity of Items found in Map"))
	assert.Equal(t, []string{"Breaches in your Maps have # Monsters"}, wordingVariants("Breaches in Map have # Monsters"))
	// Not a whole word / not a label suffix.
	for _, s := range []string{"Delirium in Maps", "# to Spirit (per socket)", "Has ()", "Dazes on Hit"} {
		assert.Empty(t, wordingVariants(s), s)
	}
	assert.Equal(t, []string{"#% increased Attack Speed per # Dexterity"}, wordingVariants("#% increased Attack Speed per 10 Dexterity"))
	assert.Equal(t, []string{"Grants Skill: Parry"}, wordingVariants("Grants Skill: Level # Parry"))
}

func TestStatsKeepSameIDWordingsAndLocalVariants(t *testing.T) {
	raw := `{"result":[{"id":"explicit","entries":[
		{"id":"explicit.xp","text":"#% increased Experience gain"},
		{"id":"explicit.xp","text":"#% increased Experience gain in Map"},
		{"id":"explicit.armour_local","text":"#% increased Armour (Local)"},
		{"id":"explicit.armour","text":"#% increased Armour"}
	]}]}`
	s, err := normaliseStats([]byte(raw))
	require.NoError(t, err)
	byID := func(id string) core.StatDef {
		t.Helper()
		i := slices.IndexFunc(s, func(d core.StatDef) bool { return d.ID == id })
		require.GreaterOrEqual(t, i, 0, "no stat %s", id)
		return s[i]
	}
	has := func(id, text string, negate bool) {
		t.Helper()
		assert.Contains(t, byID(id).Aliases, alias(text, negate), id)
	}
	assert.Equal(t, "#% increased Experience gain", byID("explicit.xp").Text)
	has("explicit.xp", "#% increased Experience gain in Map", false)
	has("explicit.xp", "#% increased Experience gain in your Maps", false)
	has("explicit.xp", "#% reduced Experience gain in your Maps", true)
	// Local variant keeps the unsuffixed wording even though a global stat has it.
	has("explicit.armour_local", "#% increased Armour", false)
	has("explicit.armour_local", "#% reduced Armour", true)
}

func TestStatsRejectsGarbage(t *testing.T) {
	for _, raw := range []string{`{}`, `{"result":null}`, `{"result":[{"id":"x"}]}`, `{"result":[{"id":"x","entries":[{"id":"a"}]}]}`, `[`} {
		_, err := normaliseStats([]byte(raw))
		assert.Error(t, err, raw)
	}
}

func TestItemsAndStaticsRejectMissingFields(t *testing.T) {
	for _, raw := range []string{`{"result":[{"id":"x","entries":[]}]}`, `{"result":[{"id":"x","label":"X","entries":[{"name":"n"}]}]}`} {
		_, err := normaliseUniques([]byte(raw))
		require.Error(t, err, raw)
	}
	for _, raw := range []string{`{"result":[{"id":"x","entries":[{"id":"a"}]}]}`, `{"result":[{"entries":[]}]}`} {
		_, err := normaliseStatics([]byte(raw))
		require.Error(t, err, raw)
	}
	_, err := normaliseStatics([]byte(`{"result":[{"id":"x","entries":[{"id":"sep","text":""}]}]}`))
	assert.NoError(t, err, "a separator's empty text is fine")
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
	bases, err := decodeRepoeBases([]byte(`{
		"Metadata/Items/Armours/Helmets/FourHelmetDex5": {"name":"Hunter Hood","item_class":"Helmet","release_state":"released"},
		"Metadata/Items/Hidden/HunterHood": {"name":"Hunter Hood","item_class":"HiddenItem","release_state":"unreleased"},
		"Metadata/Items/Rings/Ring1": {"name":"Gold Ring","item_class":"Ring","release_state":"released"}
	}`))
	require.NoError(t, err)
	var classes map[string]repoeItemClass
	require.NoError(t, json.Unmarshal([]byte(`{"Helmet":{"name":"Helmets"},"Ring":{"name":"Rings"},"HiddenItem":{}}`), &classes))
	return bases, classes
}

func TestBasesEnrichedFromRepoe(t *testing.T) {
	b, c := repoeSamples(t)
	out, err := normaliseBases([]byte(itemsJSON), b, c)
	require.NoError(t, err)
	assert.Equal(t, []core.BaseDef{
		{Name: "Gold Ring", Category: "accessory.ring", ItemClass: "Rings"},
		{Name: "Hunter Hood", Category: "armour.helmet", ItemClass: "Helmets"},
		{Name: "Mystery Plate", Category: "armour", ItemClass: "Armour"},
	}, out)
}

func TestRepoeBaseRequiresFields(t *testing.T) {
	_, err := decodeRepoeBases([]byte(`{"x":{"name":"Hunter Hood"}}`))
	require.ErrorContains(t, err, "missing field `item_class`")
	_, err = decodeRepoeBases([]byte(`{"x":{"item_class":"Helmet"}}`))
	require.ErrorContains(t, err, "missing field `name`")
	bases, err := decodeRepoeBases([]byte(`{"x":{"name":"","item_class":"Helmet"}}`))
	require.NoError(t, err, "an empty name is a real (internal) base")
	assert.Empty(t, *bases["x"].Name)
}

func TestUniquesSkipLegacy(t *testing.T) {
	out, err := normaliseUniques([]byte(itemsJSON))
	require.NoError(t, err)
	assert.Equal(t, []core.UniqueDef{{Name: "Andvarius", BaseType: "Gold Ring"}, {Name: "Elevore", BaseType: "Hunter Hood"}}, out)
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
	require.NoError(t, err)
	assert.Len(t, out, 3)
	assert.Equal(t, "chaos", out[0].ID)
	assert.Equal(t, "https://example.invalid/chaos.png", *out[0].Image)
	assert.Equal(t, "https://web.poecdn.com/gen/image/divine.png", *out[1].Image)
	assert.Equal(t, "UncutGems", out[2].Group)
	assert.Nil(t, out[2].Image)
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
		assert.Equal(t, want, got, in)
	}
}

func TestCategories(t *testing.T) {
	assert.Equal(t, "weapon.warstaff", tradeCategories["Warstaff"])
	assert.NotContains(t, tradeCategories, "QuestItem")
}
