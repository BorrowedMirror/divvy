package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapsEveryKnownHeader(t *testing.T) {
	cases := []struct {
		header string
		want   ItemClassKind
	}{
		{"Stackable Currency", ClassStackableCurrency},
		{"Omen", ClassOmen},
		{"Essence", ClassEssence},
		{"Essences", ClassEssence},
		{"Soul Cores", ClassSoulCore},
		{"Runes", ClassRune},
		{"Socketable", ClassRune},
		{"Augment", ClassRune},
		{"Distilled Emotions", ClassDistilledEmotion},
		{"Splinter", ClassSplinter},
		{"Catalysts", ClassCatalyst},
		{"Skill Gems", ClassSkillGem},
		{"Support Gems", ClassSupportGem},
		{"Uncut Skill Gems", ClassUncutGem},
		{"Uncut Support Gems", ClassUncutGem},
		{"Uncut Spirit Gems", ClassUncutGem},
		{"Waystones", ClassWaystone},
		{"Tablet", ClassTablet},
		{"Precursor Tablets", ClassTablet},
		{"Jewels", ClassJewel},
		{"Life Flasks", ClassLifeFlask},
		{"Mana Flasks", ClassManaFlask},
		{"Charms", ClassCharm},
		{"Relics", ClassRelic},
		{"Amulets", ClassAmulet},
		{"Rings", ClassRing},
		{"Belts", ClassBelt},
		{"Body Armours", ClassBodyArmour},
		{"Helmets", ClassHelmet},
		{"Gloves", ClassGloves},
		{"Boots", ClassBoots},
		{"Shields", ClassShield},
		{"Bucklers", ClassBuckler},
		{"Foci", ClassFocus},
		{"Quivers", ClassQuiver},
		{"  helmets \r", ClassHelmet},
	}
	for _, c := range cases {
		assert.Equal(t, c.want.Class(), ItemClassFromHeader(c.header), c.header)
	}
}

func TestWeaponsPreserveRawHeader(t *testing.T) {
	for _, w := range WeaponClasses {
		assert.Equal(t, WeaponClass(w), ItemClassFromHeader(w))
		assert.True(t, ItemClassFromHeader(w).IsWeapon())
	}
	assert.Equal(t, WeaponClass("Bow"), ItemClassFromHeader("Bow"))
	assert.Equal(t, WeaponClass("Staff"), ItemClassFromHeader("Staff"))
	assert.Equal(t, WeaponClass("Quarterstaff"), ItemClassFromHeader("Quarterstaff"))
}

func TestUnknownIsOther(t *testing.T) {
	assert.Equal(t, OtherClass("Pinnacle Keys"), ItemClassFromHeader("Pinnacle Keys"))
	assert.False(t, ItemClassFromHeader("Rings").IsWeapon())
}

func tradeCategory(c ItemClass) *string {
	if s, ok := c.TradeCategory(); ok {
		return &s
	}
	return nil
}

func TestWeaponTradeCategories(t *testing.T) {
	for _, w := range WeaponClasses {
		assert.NotNil(t, tradeCategory(ItemClassFromHeader(w)), "%s has no trade category", w)
	}
	assert.Nil(t, tradeCategory(WeaponClass("Lances")))
	assert.Nil(t, tradeCategory(OtherClass("x")))
	assert.Equal(t, new("gem.metagem"), tradeCategory(OtherClass("Meta Gems")))
	assert.Equal(t, new("weapon.talisman"), tradeCategory(ItemClassFromHeader("Talismans")))
	assert.Equal(t, new("armour.helmet"), tradeCategory(ClassHelmet.Class()))
}

func TestEveryClassHasCategory(t *testing.T) {
	for _, k := range unitClassKinds {
		c := k.Class()
		assert.NotNil(t, tradeCategory(c), "%v", c)
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

func rarityOf(s string) *Rarity {
	if r, ok := RarityFromHeader(s); ok {
		return &r
	}
	return nil
}

func tradeOption(r Rarity) *string {
	if s, ok := r.TradeOption(); ok {
		return &s
	}
	return nil
}

func TestRarityHeader(t *testing.T) {
	assert.Equal(t, new(RarityRare), rarityOf("Rare"))
	assert.Equal(t, new(RarityNormal), rarityOf("Normal"))
	assert.Equal(t, new(RarityMagic), rarityOf("Magic"))
	assert.Equal(t, new(RarityUnique), rarityOf("Unique"))
	assert.Equal(t, new(RarityCurrency), rarityOf("Currency"))
	assert.Equal(t, new(RarityGem), rarityOf("Gem"))
	assert.Equal(t, new(RarityOther), rarityOf("Quest"))
	assert.Nil(t, rarityOf("Legendary"))
	assert.Equal(t, new("rare"), tradeOption(RarityRare))
	assert.Equal(t, new("normal"), tradeOption(RarityNormal))
	assert.Equal(t, new("magic"), tradeOption(RarityMagic))
	assert.Equal(t, new("unique"), tradeOption(RarityUnique))
	assert.Nil(t, tradeOption(RarityGem))
}
