package core

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapsEveryKnownHeader(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected ItemClassKind
	}{
		{
			"StackableCurrency",
			"Stackable Currency",
			ClassStackableCurrency,
		},
		{
			"Omen",
			"Omen",
			ClassOmen,
		},
		{
			"Essence",
			"Essence",
			ClassEssence,
		},
		{
			"Essences",
			"Essences",
			ClassEssence,
		},
		{
			"SoulCores",
			"Soul Cores",
			ClassSoulCore,
		},
		{
			"Runes",
			"Runes",
			ClassRune,
		},
		{
			"Socketable",
			"Socketable",
			ClassRune,
		},
		{
			"Augment",
			"Augment",
			ClassRune,
		},
		{
			"DistilledEmotions",
			"Distilled Emotions",
			ClassDistilledEmotion,
		},
		{
			"Splinter",
			"Splinter",
			ClassSplinter,
		},
		{
			"Catalysts",
			"Catalysts",
			ClassCatalyst,
		},
		{
			"SkillGems",
			"Skill Gems",
			ClassSkillGem,
		},
		{
			"SupportGems",
			"Support Gems",
			ClassSupportGem,
		},
		{
			"UncutSkillGems",
			"Uncut Skill Gems",
			ClassUncutGem,
		},
		{
			"UncutSupportGems",
			"Uncut Support Gems",
			ClassUncutGem,
		},
		{
			"UncutSpiritGems",
			"Uncut Spirit Gems",
			ClassUncutGem,
		},
		{
			"Waystones",
			"Waystones",
			ClassWaystone,
		},
		{
			"Tablet",
			"Tablet",
			ClassTablet,
		},
		{
			"PrecursorTablets",
			"Precursor Tablets",
			ClassTablet,
		},
		{
			"Jewels",
			"Jewels",
			ClassJewel,
		},
		{
			"LifeFlasks",
			"Life Flasks",
			ClassLifeFlask,
		},
		{
			"ManaFlasks",
			"Mana Flasks",
			ClassManaFlask,
		},
		{
			"Charms",
			"Charms",
			ClassCharm,
		},
		{
			"Relics",
			"Relics",
			ClassRelic,
		},
		{
			"Amulets",
			"Amulets",
			ClassAmulet,
		},
		{
			"Rings",
			"Rings",
			ClassRing,
		},
		{
			"Belts",
			"Belts",
			ClassBelt,
		},
		{
			"BodyArmours",
			"Body Armours",
			ClassBodyArmour,
		},
		{
			"Helmets",
			"Helmets",
			ClassHelmet,
		},
		{
			"Gloves",
			"Gloves",
			ClassGloves,
		},
		{
			"Boots",
			"Boots",
			ClassBoots,
		},
		{
			"Shields",
			"Shields",
			ClassShield,
		},
		{
			"Bucklers",
			"Bucklers",
			ClassBuckler,
		},
		{
			"Foci",
			"Foci",
			ClassFocus,
		},
		{
			"Quivers",
			"Quivers",
			ClassQuiver,
		},
		{
			"IgnoresWhitespaceAndCase",
			"  helmets \r",
			ClassHelmet,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected.Class(), itemClassFromHeader(tc.have))
		})
	}
}

func TestWeaponsPreserveRawHeader(t *testing.T) {
	for _, w := range weaponClasses {
		assert.Equal(t, WeaponClass(w), itemClassFromHeader(w))
		assert.Equal(t, ClassWeapon, itemClassFromHeader(w).Kind)
	}
	assert.Equal(t, WeaponClass("Bow"), itemClassFromHeader("Bow"))
	assert.Equal(t, WeaponClass("Staff"), itemClassFromHeader("Staff"))
	assert.Equal(t, WeaponClass("Quarterstaff"), itemClassFromHeader("Quarterstaff"))
}

func TestUnknownIsOther(t *testing.T) {
	assert.Equal(t, OtherClass("Pinnacle Keys"), itemClassFromHeader("Pinnacle Keys"))
	assert.Equal(t, ClassRing, itemClassFromHeader("Rings").Kind)
}

func tradeCategoryOf(c ItemClass) *string {
	if s, ok := c.tradeCategory(); ok {
		return &s
	}
	return nil
}

func TestWeaponTradeCategories(t *testing.T) {
	for _, w := range weaponClasses {
		assert.NotNil(t, tradeCategoryOf(itemClassFromHeader(w)), "%s has no trade category", w)
	}
	assert.Nil(t, tradeCategoryOf(WeaponClass("Lances")))
	assert.Nil(t, tradeCategoryOf(OtherClass("x")))
	assert.Equal(t, new("gem.metagem"), tradeCategoryOf(OtherClass("Meta Gems")))
	assert.Equal(t, new("weapon.talisman"), tradeCategoryOf(itemClassFromHeader("Talismans")))
	assert.Equal(t, new("armour.helmet"), tradeCategoryOf(ClassHelmet.Class()))
}

func TestEveryClassHasCategory(t *testing.T) {
	units := []ItemClassKind{
		ClassStackableCurrency, ClassOmen, ClassEssence, ClassSoulCore, ClassRune, ClassTalisman, ClassDistilledEmotion,
		ClassSplinter, ClassCatalyst, ClassSkillGem, ClassSupportGem, ClassUncutGem, ClassWaystone, ClassTablet,
		ClassJewel, ClassLifeFlask, ClassManaFlask, ClassCharm, ClassRelic, ClassAmulet, ClassRing, ClassBelt,
		ClassBodyArmour, ClassHelmet, ClassGloves, ClassBoots, ClassShield, ClassBuckler, ClassFocus, ClassQuiver,
	}
	for _, k := range units {
		c := k.Class()
		assert.NotNil(t, tradeCategoryOf(c), "%v", c)
		_ = c.Category(RarityRare)
	}
}

func TestCategories(t *testing.T) {
	assert.Equal(t, CategoryCurrency, ClassStackableCurrency.Class().Category(RarityCurrency))
	assert.Equal(t, CategoryCurrency, ClassOmen.Class().Category(RarityUnique))
	assert.Equal(t, CategoryGem, ClassSkillGem.Class().Category(RarityGem))
	assert.Equal(t, CategoryGem, ClassUncutGem.Class().Category(RarityCurrency))
	assert.Equal(t, CategoryEquipment, ClassHelmet.Class().Category(RarityRare))
	assert.Equal(t, CategoryUnique, ClassHelmet.Class().Category(RarityUnique))
	assert.Equal(t, CategoryUnique, ClassJewel.Class().Category(RarityUnique))
	assert.Equal(t, CategoryJewel, ClassJewel.Class().Category(RarityMagic))
	assert.Equal(t, CategoryWaystone, ClassWaystone.Class().Category(RarityRare))
	assert.Equal(t, CategoryTablet, ClassTablet.Class().Category(RarityMagic))
	assert.Equal(t, CategoryFlask, ClassLifeFlask.Class().Category(RarityNormal))
	assert.Equal(t, CategoryCharm, ClassCharm.Class().Category(RarityNormal))
	assert.Equal(t, CategoryRelic, ClassRelic.Class().Category(RarityMagic))
	assert.Equal(t, CategoryUnique, ClassRelic.Class().Category(RarityUnique))
	assert.Equal(t, CategoryEquipment, WeaponClass("Bows").Category(RarityNormal))
	assert.Equal(t, CategoryCurrency, OtherClass("x").Category(RarityCurrency))
	assert.Equal(t, CategoryGem, OtherClass("x").Category(RarityGem))
	assert.Equal(t, CategoryOther, OtherClass("x").Category(RarityNormal))
	assert.Equal(t, CategoryUnique, OtherClass("x").Category(RarityUnique))
}

func TestItemClassJSON(t *testing.T) {
	assert.JSONEq(t, `{"type":"helmet"}`, marshal(t, ClassHelmet.Class()))
	assert.JSONEq(t, `{"type":"body_armour"}`, marshal(t, ClassBodyArmour.Class()))
	assert.JSONEq(t, `{"type":"weapon","value":"Bows"}`, marshal(t, WeaponClass("Bows")))
	assert.JSONEq(t, `{"type":"other","value":"Meta Gems"}`, marshal(t, OtherClass("Meta Gems")))
	for _, c := range []ItemClass{ClassSoulCore.Class(), WeaponClass("Bows"), OtherClass("x")} {
		var back ItemClass
		require.NoError(t, json.Unmarshal([]byte(marshal(t, c)), &back))
		assert.Equal(t, c, back)
	}
	var c ItemClass
	require.Error(t, json.Unmarshal([]byte(`{"type":"lance"}`), &c))
	require.Error(t, json.Unmarshal([]byte(`{"type":"weapon"}`), &c))
	require.Error(t, json.Unmarshal([]byte(`{}`), &c))
}

func TestEnumsRejectUnknownValues(t *testing.T) {
	var r Rarity
	require.NoError(t, json.Unmarshal([]byte(`"rare"`), &r))
	assert.Equal(t, RarityRare, r)
	require.Error(t, json.Unmarshal([]byte(`"Rare"`), &r))
	var k ModKind
	require.Error(t, json.Unmarshal([]byte(`"bogus"`), &k))
	var cat ItemCategory
	require.Error(t, json.Unmarshal([]byte(`"bogus"`), &cat))
	var a Affix
	require.NoError(t, json.Unmarshal([]byte(`"suffix"`), &a))
	var s PriceSourceKind
	require.NoError(t, json.Unmarshal([]byte(`"poe2_scout"`), &s))
	assert.Equal(t, SourcePoe2Scout, s)
	var st OnlineStatus
	require.EqualError(t, st.UnmarshalText([]byte("offline")), `unknown core.OnlineStatus "offline"`)
	var route QueryRoute
	require.Error(t, json.Unmarshal([]byte(`"bulk"`), &route))
	assert.JSONEq(t, `"poe_ninja"`, marshal(t, SourcePoeNinja))
}
