package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestSmokeLooksLikeItem(t *testing.T) {
	assert.True(t, LooksLikeItem("Item Class: Stackable Currency\nRarity: Currency\nDivine Orb"))
	assert.True(t, LooksLikeItem("Rarity: Gem\nCoil of Fire"))
	assert.False(t, LooksLikeItem("hello"))
}

func parseErr(text string) error {
	_, err := ParseItem(text)
	return err
}

func TestErrors(t *testing.T) {
	require.ErrorIs(t, parseErr("  \n"), ErrEmpty)
	require.ErrorIs(t, parseErr("hello world"), ErrNotAnItem)
	require.ErrorIs(t, parseErr("Item Class: Rings"), ErrNotAnItem)
	require.ErrorIs(t, parseErr("Item Class: Rings\nBogus"), ErrNotAnItem)
	require.ErrorIs(t, parseErr("Rarity: Rare\nFoo"), ErrNotAnItem)
	require.ErrorIs(t, parseErr("--------\n--------"), ErrEmpty)

	err := parseErr("Item Class: Rings\nRarity: Legendary\nX")
	rarity, ok := errors.AsType[*UnknownRarityError](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "Legendary", rarity.Value)
	require.EqualError(t, err, "unknown rarity `Legendary`")

	err = parseErr("Item Class: Rings\nRarity: Rare")
	malformed, ok := errors.AsType[*MalformedError](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, &MalformedError{Section: "header", Reason: "missing item name"}, malformed)
	require.EqualError(t, err, "malformed section `header`: missing item name")
	require.EqualError(t, ErrEmpty, "item text is empty")
}

const rareHelm = `Item Class: Helmets
Rarity: Rare
Doom Visor
Superior Expert Hunter Hood
--------
Quality: +20% (augmented)
Evasion Rating: 312 (augmented)
--------
Requires: Level 65, 87 (unmet) Dex
--------
Sockets: S S
--------
Item Level: 79
--------
+12% to Fire Resistance (rune)
--------
+42 to maximum Life
+31% to Cold Resistance
Totally made-up line (fractured)
--------
Corrupted
Mirrored
--------
Note: ~price 2 divine
`

func mustParse(tb testing.TB, text string) *ParsedItem {
	tb.Helper()
	item, err := ParseItem(text)
	require.NoError(tb, err)
	return item
}

func TestRareHelmet(t *testing.T) {
	item := mustParse(t, rareHelm)
	assert.Equal(t, ClassHelmet.Class(), item.Class)
	assert.Equal(t, CategoryEquipment, item.Category)
	assert.Equal(t, new("Doom Visor"), item.Name)
	assert.Equal(t, "Superior Expert Hunter Hood", item.BaseType)
	assert.Equal(t, new(uint32(20)), item.Quality)
	assert.Equal(t, new(uint32(312)), item.Stats.Evasion)
	assert.Equal(t, Requirements{Level: new(uint32(65)), Dex: new(uint32(87))}, item.Requirements)
	assert.Equal(t, uint8(2), item.Sockets)
	assert.Equal(t, new(uint32(79)), item.ItemLevel)
	require.Len(t, item.Modifiers, 4)
	assert.Equal(t, ModRune, item.Modifiers[0].Kind)
	assert.Equal(t, "+12% to Fire Resistance", item.Modifiers[0].Text)
	assert.Equal(t, "+# to maximum Life", item.Modifiers[1].Template)
	assert.Equal(t, []float64{42}, item.Modifiers[1].Values)
	assert.Equal(t, ModFractured, item.Modifiers[3].Kind)
	assert.True(t, item.Corrupted && item.Mirrored && !item.Unidentified)
	assert.Equal(t, new("~price 2 divine"), item.Note)
	assert.True(t, item.Properties[0].Augmented)
	assert.False(t, item.Advanced)
}

// The game puts "You cannot use this item…" after the rarity of an item the character can't use
// (unmet requirements, the inactive weapon set); it parses as the same item.
func TestUnusableItem(t *testing.T) {
	notice := "You cannot use this item. Its stats will be ignored"
	testCases := []struct {
		name string
		have string
	}{
		{
			"OwnSection",
			strings.Replace(rareHelm, "Rarity: Rare\n", "Rarity: Rare\n"+notice+"\n--------\n", 1),
		},
		{
			"BeforeTheName",
			strings.Replace(rareHelm, "Rarity: Rare\n", "Rarity: Rare\n"+notice+"\n", 1),
		},
	}

	expected := mustParse(t, rareHelm)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := mustParse(t, tc.have)
			assert.Equal(t, new("Doom Visor"), actual.Name)
			assert.Equal(t, "Superior Expert Hunter Hood", actual.BaseType)
			actual.Raw = expected.Raw
			assert.Equal(t, expected, actual)
		})
	}
}

func TestCRLFMatchesLF(t *testing.T) {
	crlf := strings.ReplaceAll(rareHelm, "\n", "\r\n")
	a := mustParse(t, rareHelm)
	b := mustParse(t, crlf)
	a.Raw = ""
	b.Raw = ""
	assert.Equal(t, a, b)
}

func TestMultiLineRequirementsAndSuperior(t *testing.T) {
	text := "Item Class: Gloves\nRarity: Normal\nSuperior Stocky Mitts\n--------\nRequirements:\nLevel: 12\nStr: 20\nDexterity: 5\nInt: 7\nBogus\n--------\nItem Level: 15"
	item := mustParse(t, text)
	assert.Equal(t, "Stocky Mitts", item.BaseType)
	assert.Equal(t, Requirements{Level: new(uint32(12)), Str: new(uint32(20)), Dex: new(uint32(5)), Int: new(uint32(7))},
		item.Requirements)
	var req Requirements
	parseRequirementLine(&req, "Requires: Str 30, 40, Strange Thing Here, 50 Intelligence")
	assert.Equal(t, Requirements{Level: new(uint32(40)), Str: new(uint32(30)), Int: new(uint32(50))}, req)
}

func TestWeaponDPS(t *testing.T) {
	text := "Item Class: Bows\nRarity: Rare\nHate Thirst\nWarmonger Bow\n--------\nPhysical Damage: 50-100 (augmented)\nElemental Damage: 10-20 (fire), 5-15 (cold)\nLightning Damage: 1-9 (lightning)\nChaos Damage: 2-4\nCritical Hit Chance: 5.00%\nAttacks per Second: 1.20\n--------\nItem Level: 80\n--------\nAdds 1 to 4 Lightning Damage"
	item := mustParse(t, text)
	assert.Equal(t, new(90.0), item.Stats.PhysicalDPS)
	assert.Equal(t, new(36.0), item.Stats.ElementalDPS)
	assert.Equal(t, new(129.6), item.Stats.TotalDPS)
	assert.Equal(t, new(5.0), item.Stats.CriticalHitChance)
	// Without APS no DPS is reported.
	noAPS := mustParse(t, "Item Class: Bows\nRarity: Normal\nShort Bow\n--------\nPhysical Damage: 5-10")
	assert.Nil(t, noAPS.Stats.PhysicalDPS)
}

func TestAdvancedCopy(t *testing.T) {
	text := "Item Class: Rings\nRarity: Rare\nRapture Loop\nRuby Ring\n--------\nItem Level: 81\n--------\n{ Implicit Modifier — Elemental, Fire, Resistance }\n+25(20-30)% to Fire Resistance\n--------\n{ Prefix Modifier \"Hale\" (Tier: 7) — Life }\n+42(40-49) to maximum Life\n(Reminder text here)\n{ Suffix Modifier \"of Infamy\" — Attack, Speed }\n14(15-10)% reduced Attack Speed — Unscalable Value\n{ Fractured Prefix Modifier \"Electrocuting\" (Rank: 2) — Lightning }\nAdds 1(1-4) to 50(46-66) Lightning Damage to Attacks\n{ Desecrated Suffix Modifier \"of Amanamu\" (Tier: 1) }\n+10% to Chaos Resistance\n{ Crafted Suffix Modifier }\n+5 to Strength\n{ Corruption Enhancement }\n+1 to Level of all Skills\n{ Unique Modifier — Defences }\n+3 to Spirit\n{ Rune Modifier }\n+2 to Dexterity\nDesecrated Prefix\n"
	item := mustParse(t, text)
	assert.True(t, item.Advanced)
	m := item.Modifiers
	require.Len(t, m, 9)
	assert.Equal(t, ModImplicit, m[0].Kind)
	assert.Equal(t, "+25% to Fire Resistance", m[0].Text)
	adv := m[0].Advanced
	require.NotNil(t, adv)
	assert.Equal(t, new(AffixImplicit), adv.Affix)
	assert.Equal(t, []ValueRange{{Min: 20, Max: 30}}, adv.Ranges)
	hale := m[1].Advanced
	assert.Equal(t, new(AffixPrefix), hale.Affix)
	assert.Equal(t, new("Hale"), hale.Name)
	assert.Equal(t, new(uint8(7)), hale.Tier)
	assert.Equal(t, "14% reduced Attack Speed", m[2].Text)
	assert.Equal(t, []ValueRange{{Min: 10, Max: 15}}, m[2].Advanced.Ranges)
	assert.Equal(t, new(AffixSuffix), m[2].Advanced.Affix)
	assert.Equal(t, ModFractured, m[3].Kind)
	assert.Len(t, m[3].Advanced.Ranges, 2)
	assert.Equal(t, new(uint8(2)), m[3].Advanced.Tier)
	assert.Equal(t, ModDesecrated, m[4].Kind)
	assert.Equal(t, ModCrafted, m[5].Kind)
	assert.Equal(t, ModEnchant, m[6].Kind)
	assert.Equal(t, ModExplicit, m[7].Kind)
	assert.Nil(t, m[7].Advanced.Affix)
	assert.Equal(t, ModRune, m[8].Kind)
}

func TestUniqueFlavourAndSkills(t *testing.T) {
	text := "Item Class: Sceptres\nRarity: Unique\nThe Dark Defiler\nRattling Sceptre\n--------\nSpirit: 100\n--------\nItem Level: 70\n--------\nGrants Skill: Level 12 Skeletal Warrior\n--------\n+20 to Intelligence\n--------\nA dark gift.\n--------\nUnidentified (Tier 4)"
	item := mustParse(t, text)
	assert.Equal(t, CategoryUnique, item.Category)
	assert.Equal(t, new(uint32(100)), item.Stats.Spirit)
	require.Len(t, item.Modifiers, 2)
	assert.Equal(t, ModSkill, item.Modifiers[0].Kind)
	assert.True(t, item.Unidentified)
}

func TestMetaGemAndRefinements(t *testing.T) {
	gem := mustParse(t, "Rarity: Gem\nCast on Critical\n--------\nLevel: 5\n--------\nSome text")
	assert.Equal(t, OtherClass("Meta Gems"), gem.Class)
	assert.Equal(t, CategoryGem, gem.Category)
	assert.Equal(t, new(uint32(5)), gem.GemLevel)
	uncut := mustParse(t, "Item Class: Uncut Skill Gems\nRarity: Currency\nUncut Skill Gem (Level 19)")
	assert.Equal(t, new(uint32(19)), uncut.GemLevel)
	way := mustParse(t, "Item Class: Waystones\nRarity: Normal\nWaystone (Tier 15)")
	assert.Equal(t, new(uint32(15)), way.WaystoneTier)
	core := mustParse(t, "Item Class: Augment\nRarity: Currency\nSoul Core of Tacati")
	assert.Equal(t, ClassSoulCore.Class(), core.Class)

	testCases := []struct {
		name     string
		have     string
		expected ItemClassKind
	}{
		{
			"Essence",
			"Greater Essence of the Body",
			ClassEssence,
		},
		{
			"DistilledEmotion",
			"Distilled Ire",
			ClassDistilledEmotion,
		},
		{
			"Splinter",
			"Breach Splinter",
			ClassSplinter,
		},
		{
			"Catalyst",
			"Flesh Catalyst",
			ClassCatalyst,
		},
		{
			"Omen",
			"Omen of Amelioration",
			ClassOmen,
		},
		{
			"StackableCurrency",
			"Exalted Orb",
			ClassStackableCurrency,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			item := mustParse(t, "Item Class: Stackable Currency\nRarity: Currency\n"+tc.have)
			assert.Equal(t, tc.expected.Class(), item.Class)
		})
	}
}

func TestStackSizeAndAreaLevel(t *testing.T) {
	item := mustParse(t, "Item Class: Stackable Currency\nRarity: Currency\nExalted Orb\n--------\nStack Size: 1,234/5,000\nArea Level: 70")
	assert.Equal(t, &StackSize{Current: 1234, Max: 5000}, item.StackSize)
	assert.Equal(t, new(uint32(70)), item.AreaLevel)
}

// The patterns find numbers in any script (`\p{Nd}`), but only ASCII digits parse, so the
// others are dropped.
func TestUnicodeDigitsAreTemplatedButNotParsed(t *testing.T) {
	assert.Equal(t, "+# to maximum Life", Templatize("+٣ to maximum Life"))
	assert.Empty(t, extractValues("+٣ to maximum Life"))
	item := mustParse(t, "Item Class: Rings\nRarity: Rare\nX\nRuby Ring\n--------\nItem Level: ٨٠")
	assert.Nil(t, item.ItemLevel)
}

func TestNeverPanics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringOfN(anyRune(), 0, 400, -1).Draw(t, "s")
		_, _ = ParseItem(s)
	})
}

func TestNeverPanicsStructured(t *testing.T) {
	line := rapid.OneOf(
		rapid.Just("--------"),
		rapid.Just("Item Class: Helmets"),
		rapid.Just("Rarity: Rare"),
		rapid.Just("{ Prefix Modifier \"X\" (Tier: 1) — Y }"),
		rapid.Just("Requires: Level"),
		rapid.Just("Sockets:"),
		rapid.StringMatching(`[ -~]{0,40}`),
		rapid.StringMatching(`[0-9()+%:, -]{0,20}`),
	)
	rapid.Check(t, func(t *rapid.T) {
		lines := rapid.SliceOfN(line, 0, 29).Draw(t, "lines")
		_, _ = ParseItem(strings.Join(lines, "\n"))
	})
}
