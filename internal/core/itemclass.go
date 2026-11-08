package core

import (
	"encoding/json/v2"
	"errors"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/enum"
)

// ItemCategory is a coarse category used for routing (which price source, which filters).
type ItemCategory string

// ItemCategory values.
const (
	CategoryCurrency  ItemCategory = "currency"
	CategoryGem       ItemCategory = "gem"
	CategoryUnique    ItemCategory = "unique"
	CategoryEquipment ItemCategory = "equipment"
	CategoryWaystone  ItemCategory = "waystone"
	CategoryTablet    ItemCategory = "tablet"
	CategoryJewel     ItemCategory = "jewel"
	CategoryFlask     ItemCategory = "flask"
	CategoryCharm     ItemCategory = "charm"
	CategoryRelic     ItemCategory = "relic"
	CategoryOther     ItemCategory = "other"
)

// UnmarshalText accepts only the known categories.
func (c *ItemCategory) UnmarshalText(text []byte) error {
	return enum.Parse(c, text, CategoryCurrency, CategoryGem, CategoryUnique, CategoryEquipment,
		CategoryWaystone, CategoryTablet, CategoryJewel, CategoryFlask, CategoryCharm, CategoryRelic, CategoryOther)
}

// ItemClassKind is the kind of an [ItemClass] (its JSON `type`).
type ItemClassKind string

// ItemClassKind values.
const (
	ClassStackableCurrency ItemClassKind = "stackable_currency"
	ClassOmen              ItemClassKind = "omen"
	ClassEssence           ItemClassKind = "essence"
	ClassSoulCore          ItemClassKind = "soul_core"
	ClassRune              ItemClassKind = "rune"
	ClassTalisman          ItemClassKind = "talisman"
	ClassDistilledEmotion  ItemClassKind = "distilled_emotion"
	ClassSplinter          ItemClassKind = "splinter"
	ClassCatalyst          ItemClassKind = "catalyst"
	ClassSkillGem          ItemClassKind = "skill_gem"
	ClassSupportGem        ItemClassKind = "support_gem"
	ClassUncutGem          ItemClassKind = "uncut_gem"
	ClassWaystone          ItemClassKind = "waystone"
	ClassTablet            ItemClassKind = "tablet"
	ClassJewel             ItemClassKind = "jewel"
	ClassLifeFlask         ItemClassKind = "life_flask"
	ClassManaFlask         ItemClassKind = "mana_flask"
	ClassCharm             ItemClassKind = "charm"
	ClassRelic             ItemClassKind = "relic"
	ClassAmulet            ItemClassKind = "amulet"
	ClassRing              ItemClassKind = "ring"
	ClassBelt              ItemClassKind = "belt"
	ClassBodyArmour        ItemClassKind = "body_armour"
	ClassHelmet            ItemClassKind = "helmet"
	ClassGloves            ItemClassKind = "gloves"
	ClassBoots             ItemClassKind = "boots"
	ClassShield            ItemClassKind = "shield"
	ClassBuckler           ItemClassKind = "buckler"
	ClassFocus             ItemClassKind = "focus"
	ClassQuiver            ItemClassKind = "quiver"
	// ClassWeapon covers weapon subclasses ("Bows", "Two Hand Maces", ...); the raw header
	// value is preserved in [ItemClass.Value].
	ClassWeapon ItemClassKind = "weapon"
	// ClassOther is an unmapped header value, preserved verbatim in [ItemClass.Value].
	ClassOther ItemClassKind = "other"
)

// UnmarshalText accepts only the known kinds.
func (k *ItemClassKind) UnmarshalText(text []byte) error {
	return enum.Parse(k, text,
		ClassStackableCurrency, ClassOmen, ClassEssence, ClassSoulCore, ClassRune, ClassTalisman, ClassDistilledEmotion,
		ClassSplinter, ClassCatalyst, ClassSkillGem, ClassSupportGem, ClassUncutGem, ClassWaystone, ClassTablet,
		ClassJewel, ClassLifeFlask, ClassManaFlask, ClassCharm, ClassRelic, ClassAmulet, ClassRing, ClassBelt,
		ClassBodyArmour, ClassHelmet, ClassGloves, ClassBoots, ClassShield, ClassBuckler, ClassFocus, ClassQuiver,
		ClassWeapon, ClassOther)
}

// ItemClass is the exact `Item Class:` header value, normalised.
//
// Its JSON is `{"type":"helmet"}`, or `{"type":"weapon","value":"Bows"}` for the
// [ClassWeapon] and [ClassOther] kinds. Value is only meaningful for those two kinds and
// must be empty otherwise.
type ItemClass struct {
	Kind  ItemClassKind
	Value string
}

// Class returns the [ItemClass] of this kind (for [ClassWeapon] / [ClassOther] use
// [WeaponClass] / [OtherClass]).
func (k ItemClassKind) Class() ItemClass { return ItemClass{Kind: k} }

// WeaponClass is the weapon class with the raw header value (`Bows`).
func WeaponClass(raw string) ItemClass { return ItemClass{Kind: ClassWeapon, Value: raw} }

// OtherClass is an unmapped class with the raw header value.
func OtherClass(raw string) ItemClass { return ItemClass{Kind: ClassOther, Value: raw} }

func (c ItemClass) hasValue() bool { return c.Kind == ClassWeapon || c.Kind == ClassOther }

type itemClassJSON struct {
	Type  ItemClassKind `json:"type"`
	Value *string       `json:"value,omitzero"`
}

// MarshalJSON writes `{"type":...}`, with `value` for the weapon and other kinds.
func (c ItemClass) MarshalJSON() ([]byte, error) {
	w := itemClassJSON{Type: c.Kind}
	if c.hasValue() {
		w.Value = &c.Value
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads `{"type":...}`; the weapon and other kinds require `value`.
func (c *ItemClass) UnmarshalJSON(b []byte) error {
	var w itemClassJSON
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	out := ItemClass{Kind: w.Type}
	if out.hasValue() {
		if w.Value == nil {
			return errors.New("item class " + string(w.Type) + " has no value")
		}
		out.Value = *w.Value
	}
	if out.Kind == "" {
		return errors.New("item class has no type")
	}
	*c = out
	return nil
}

// weaponClasses are the weapon class headers as printed by PoE2 (plural form).
var weaponClasses = []string{
	"Bows",
	"Crossbows",
	"Wands",
	"Staves",
	"Warstaves",
	"Quarterstaves",
	"Talismans",
	"One Hand Maces",
	"Two Hand Maces",
	"One Hand Swords",
	"Two Hand Swords",
	"One Hand Axes",
	"Two Hand Axes",
	"Sceptres",
	"Spears",
	"Flails",
	"Daggers",
	"Claws",
	"Traps",
	"Fishing Rods",
}

// itemClassFromHeader maps an `Item Class:` header value (e.g. "Stackable Currency") to an
// [ItemClass].
//
// Matching is case-insensitive and tolerant of singular/plural spellings. Unknown values
// are preserved in [ClassOther]; weapon subclasses keep their raw header in [ClassWeapon].
func itemClassFromHeader(value string) ItemClass {
	raw := strings.TrimSpace(value)
	key := strings.ToLower(raw)
	var k ItemClassKind
	switch key {
	case "stackable currency", "currency":
		k = ClassStackableCurrency
	case "omen", "omens":
		k = ClassOmen
	case "essence", "essences":
		k = ClassEssence
	case "soul core", "soul cores":
		k = ClassSoulCore
	case "rune", "runes", "augment", "augments", "socketable", "socketables":
		k = ClassRune
	case "talisman socketable", "talisman socketables":
		k = ClassTalisman
	case "distilled emotion", "distilled emotions":
		k = ClassDistilledEmotion
	case "splinter", "splinters", "breach splinter", "breach splinters":
		k = ClassSplinter
	case "catalyst", "catalysts", "breach catalyst", "breach catalysts":
		k = ClassCatalyst
	case "skill gem", "skill gems", "active skill gems":
		k = ClassSkillGem
	case "support gem", "support gems":
		k = ClassSupportGem
	case "uncut skill gem", "uncut skill gems", "uncut support gem", "uncut support gems",
		"uncut spirit gem", "uncut spirit gems", "uncut gems":
		k = ClassUncutGem
	case "waystone", "waystones", "map", "maps":
		k = ClassWaystone
	case "tablet", "tablets", "precursor tablet", "precursor tablets":
		k = ClassTablet
	case "jewel", "jewels":
		k = ClassJewel
	case "life flask", "life flasks":
		k = ClassLifeFlask
	case "mana flask", "mana flasks":
		k = ClassManaFlask
	case "charm", "charms", "utility flask", "utility flasks":
		k = ClassCharm
	case "relic", "relics":
		k = ClassRelic
	case "amulet", "amulets":
		k = ClassAmulet
	case "ring", "rings":
		k = ClassRing
	case "belt", "belts":
		k = ClassBelt
	case "body armour", "body armours":
		k = ClassBodyArmour
	case "helmet", "helmets":
		k = ClassHelmet
	case "glove", "gloves":
		k = ClassGloves
	case "boot", "boots":
		k = ClassBoots
	case "shield", "shields":
		k = ClassShield
	case "buckler", "bucklers":
		k = ClassBuckler
	case "focus", "foci":
		k = ClassFocus
	case "quiver", "quivers":
		k = ClassQuiver
	default:
		for _, w := range weaponClasses {
			if strings.EqualFold(w, raw) {
				return WeaponClass(raw)
			}
		}
		if isWeaponSingular(key) {
			return WeaponClass(raw)
		}
		return OtherClass(raw)
	}
	return ItemClass{Kind: k}
}

// Category used for routing.
//
// Currency-like and gem classes keep their category regardless of rarity; any other
// unique item routes as [CategoryUnique].
func (c ItemClass) Category(rarity Rarity) ItemCategory {
	var base ItemCategory
	switch c.Kind {
	case ClassStackableCurrency, ClassOmen, ClassEssence, ClassSoulCore, ClassRune, ClassTalisman,
		ClassDistilledEmotion, ClassSplinter, ClassCatalyst:
		return CategoryCurrency
	case ClassSkillGem, ClassSupportGem, ClassUncutGem:
		return CategoryGem
	case ClassWaystone:
		base = CategoryWaystone
	case ClassTablet:
		base = CategoryTablet
	case ClassJewel:
		base = CategoryJewel
	case ClassLifeFlask, ClassManaFlask:
		base = CategoryFlask
	case ClassCharm:
		base = CategoryCharm
	case ClassRelic:
		base = CategoryRelic
	case ClassAmulet, ClassRing, ClassBelt, ClassBodyArmour, ClassHelmet, ClassGloves, ClassBoots, ClassShield,
		ClassBuckler, ClassFocus, ClassQuiver, ClassWeapon:
		base = CategoryEquipment
	default: // ClassOther
		switch rarity {
		case RarityCurrency:
			return CategoryCurrency
		case RarityGem:
			return CategoryGem
		default:
			base = CategoryOther
		}
	}
	if rarity == RarityUnique {
		return CategoryUnique
	}
	return base
}

// tradeCategory is the trade2 `type_filters.category` option for this class, when one
// exists ("" and false otherwise).
//
// Ids follow `/api/trade2/data/filters` (`weapon.bow`, `armour.helmet`, ...).
func (c ItemClass) tradeCategory() (string, bool) {
	switch c.Kind {
	case ClassStackableCurrency, ClassCatalyst, ClassEssence, ClassDistilledEmotion, ClassSplinter:
		return "currency", true
	case ClassOmen:
		return "currency.omen", true
	case ClassSoulCore:
		return "currency.soulcore", true
	case ClassRune:
		return "currency.rune", true
	case ClassTalisman:
		return "currency.socketable", true
	case ClassSkillGem:
		return "gem.activegem", true
	case ClassSupportGem:
		return "gem.supportgem", true
	case ClassUncutGem:
		return "gem", true
	case ClassWaystone:
		return "map.waystone", true
	case ClassTablet:
		return "map.tablet", true
	case ClassJewel:
		return "jewel", true
	case ClassLifeFlask:
		return "flask.life", true
	case ClassManaFlask:
		return "flask.mana", true
	case ClassCharm:
		return "flask.charm", true
	case ClassRelic:
		return "sanctum.relic", true
	case ClassAmulet:
		return "accessory.amulet", true
	case ClassRing:
		return "accessory.ring", true
	case ClassBelt:
		return "accessory.belt", true
	case ClassBodyArmour:
		return "armour.chest", true
	case ClassHelmet:
		return "armour.helmet", true
	case ClassGloves:
		return "armour.gloves", true
	case ClassBoots:
		return "armour.boots", true
	case ClassShield:
		return "armour.shield", true
	case ClassBuckler:
		return "armour.buckler", true
	case ClassFocus:
		return "armour.focus", true
	case ClassQuiver:
		return "armour.quiver", true
	case ClassWeapon:
		return weaponTradeCategory(c.Value)
	case ClassOther:
		if strings.EqualFold(c.Value, "Meta Gems") {
			return "gem.metagem", true
		}
	}
	return "", false
}

func weaponTradeCategory(raw string) (string, bool) {
	key := strings.TrimRight(strings.ToLower(raw), "s")
	switch key {
	case "bow":
		return "weapon.bow", true
	case "crossbow":
		return "weapon.crossbow", true
	case "wand":
		return "weapon.wand", true
	case "stave", "staff":
		return "weapon.staff", true
	case "quarterstave", "quarterstaff", "warstave", "warstaff":
		return "weapon.warstaff", true
	case "one hand mace":
		return "weapon.onemace", true
	case "two hand mace":
		return "weapon.twomace", true
	case "one hand sword":
		return "weapon.onesword", true
	case "two hand sword":
		return "weapon.twosword", true
	case "one hand axe":
		return "weapon.oneaxe", true
	case "two hand axe":
		return "weapon.twoaxe", true
	case "sceptre":
		return "weapon.sceptre", true
	case "spear":
		return "weapon.spear", true
	case "flail":
		return "weapon.flail", true
	case "dagger":
		return "weapon.dagger", true
	case "claw":
		return "weapon.claw", true
	case "trap":
		return "weapon.trap", true
	case "talisman":
		return "weapon.talisman", true
	case "fishing rod":
		return "weapon.rod", true
	}
	return "", false
}

func isWeaponSingular(key string) bool {
	for _, w := range weaponClasses {
		w = strings.ToLower(w)
		var singular string
		if s, ok := strings.CutSuffix(w, "ves"); ok {
			singular = s + "ff"
		} else if s, ok := strings.CutSuffix(w, "s"); ok {
			singular = s
		}
		if singular == key {
			return true
		}
	}
	return false
}
