package core

import (
	"encoding/json"
	"fmt"
)

// Rarity is the rarity line (`Rarity: ...`) of an item.
type Rarity string

// Rarity values, serialised in snake_case like the Rust enum.
const (
	RarityNormal   Rarity = "normal"
	RarityMagic    Rarity = "magic"
	RarityRare     Rarity = "rare"
	RarityUnique   Rarity = "unique"
	RarityCurrency Rarity = "currency"
	RarityGem      Rarity = "gem"
	// RarityOther covers quest items, divination-like or other.
	RarityOther Rarity = "other"
)

// UnmarshalJSON accepts only the known variants, like serde.
func (r *Rarity) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, r, "Rarity", RarityNormal, RarityMagic, RarityRare, RarityUnique, RarityCurrency, RarityGem, RarityOther)
}

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

// UnmarshalJSON accepts only the known variants, like serde.
func (c *ItemCategory) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, c, "ItemCategory", CategoryCurrency, CategoryGem, CategoryUnique, CategoryEquipment,
		CategoryWaystone, CategoryTablet, CategoryJewel, CategoryFlask, CategoryCharm, CategoryRelic, CategoryOther)
}

// ItemClassKind is the variant of an [ItemClass] (its serde `type` tag).
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

var unitClassKinds = []ItemClassKind{
	ClassStackableCurrency, ClassOmen, ClassEssence, ClassSoulCore, ClassRune, ClassTalisman, ClassDistilledEmotion,
	ClassSplinter, ClassCatalyst, ClassSkillGem, ClassSupportGem, ClassUncutGem, ClassWaystone, ClassTablet, ClassJewel,
	ClassLifeFlask, ClassManaFlask, ClassCharm, ClassRelic, ClassAmulet, ClassRing, ClassBelt, ClassBodyArmour,
	ClassHelmet, ClassGloves, ClassBoots, ClassShield, ClassBuckler, ClassFocus, ClassQuiver,
}

// ItemClass is the exact `Item Class:` header value, normalised.
//
// It mirrors an adjacently tagged Rust enum: unit variants serialise as `{"type":"helmet"}`,
// the [ClassWeapon] and [ClassOther] variants as `{"type":"weapon","value":"Bows"}`.
// Value is only meaningful for those two kinds and must be empty otherwise.
type ItemClass struct {
	Kind  ItemClassKind
	Value string
}

// Class returns the unit [ItemClass] of this kind (for [ClassWeapon] / [ClassOther] use
// [WeaponClass] / [OtherClass]).
func (k ItemClassKind) Class() ItemClass { return ItemClass{Kind: k} }

// WeaponClass is `ItemClass::Weapon(raw)`.
func WeaponClass(raw string) ItemClass { return ItemClass{Kind: ClassWeapon, Value: raw} }

// OtherClass is `ItemClass::Other(raw)`.
func OtherClass(raw string) ItemClass { return ItemClass{Kind: ClassOther, Value: raw} }

func (c ItemClass) hasValue() bool { return c.Kind == ClassWeapon || c.Kind == ClassOther }

// String renders like Rust's Debug output (`Helmet`, `Weapon("Bows")`).
func (c ItemClass) String() string {
	name := c.Kind.VariantName()
	if c.hasValue() {
		return fmt.Sprintf("%s(%q)", name, c.Value)
	}
	return name
}

// VariantName is the Rust variant name (`body_armour` -> `BodyArmour`).
func (k ItemClassKind) VariantName() string {
	out := make([]byte, 0, len(k))
	upper := true
	for i := 0; i < len(k); i++ {
		b := k[i]
		if b == '_' {
			upper = true
			continue
		}
		if upper && b >= 'a' && b <= 'z' {
			b -= 'a' - 'A'
		}
		upper = false
		out = append(out, b)
	}
	return string(out)
}

// MarshalJSON writes the adjacently tagged serde form.
func (c ItemClass) MarshalJSON() ([]byte, error) {
	buf := []byte(`{"type":`)
	buf = appendJSONString(buf, string(c.Kind))
	if c.hasValue() {
		buf = append(buf, `,"value":`...)
		buf = appendJSONString(buf, c.Value)
	}
	return append(buf, '}'), nil
}

// UnmarshalJSON reads the adjacently tagged serde form.
func (c *ItemClass) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type  *string          `json:"type"`
		Value *json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type == nil {
		return fmt.Errorf("ItemClass: missing field `type`")
	}
	kind := ItemClassKind(*raw.Type)
	switch kind {
	case ClassWeapon, ClassOther:
		if raw.Value == nil {
			return fmt.Errorf("ItemClass: missing field `value`")
		}
		var v string
		if err := json.Unmarshal(*raw.Value, &v); err != nil {
			return err
		}
		*c = ItemClass{Kind: kind, Value: v}
		return nil
	}
	for _, k := range unitClassKinds {
		if k == kind {
			*c = ItemClass{Kind: kind}
			return nil
		}
	}
	return fmt.Errorf("ItemClass: unknown variant `%s`", kind)
}

// ModKind says which block of the item the modifier came from.
type ModKind string

// ModKind values.
const (
	ModImplicit   ModKind = "implicit"
	ModExplicit   ModKind = "explicit"
	ModEnchant    ModKind = "enchant"
	ModRune       ModKind = "rune"
	ModCrafted    ModKind = "crafted"
	ModFractured  ModKind = "fractured"
	ModDesecrated ModKind = "desecrated"
	ModSanctified ModKind = "sanctified"
	ModSkill      ModKind = "skill"
)

// UnmarshalJSON accepts only the known variants, like serde.
func (k *ModKind) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, k, "ModKind", ModImplicit, ModExplicit, ModEnchant, ModRune, ModCrafted, ModFractured,
		ModDesecrated, ModSanctified, ModSkill)
}

// AdvancedModInfo is prefix / suffix information only available in advanced (Ctrl+Alt+C) copies.
type AdvancedModInfo struct {
	Affix *Affix  `json:"affix"`
	Name  *string `json:"name"`
	Tier  *uint8  `json:"tier"`
	// Value ranges as printed, e.g. `(10-20)` -> `[10.0, 20.0]`, per value.
	Ranges []ValueRange `json:"ranges"`
}

// MarshalJSON writes serde-compatible JSON.
func (a AdvancedModInfo) MarshalJSON() ([]byte, error) { return marshalStruct(a) }

// Affix is the kind of an advanced mod header.
type Affix string

// Affix values.
const (
	AffixPrefix   Affix = "prefix"
	AffixSuffix   Affix = "suffix"
	AffixImplicit Affix = "implicit"
)

// UnmarshalJSON accepts only the known variants, like serde.
func (a *Affix) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, a, "Affix", AffixPrefix, AffixSuffix, AffixImplicit)
}

// ValueRange is a printed roll range.
type ValueRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// MarshalJSON writes serde-compatible JSON.
func (v ValueRange) MarshalJSON() ([]byte, error) { return marshalStruct(v) }

// Modifier is one modifier line.
type Modifier struct {
	// Line text exactly as copied (minus `(implicit)`-style suffix markers).
	Text string `json:"text"`
	// Text with numbers replaced by `#`, used to look up stat ids.
	Template string  `json:"template"`
	Kind     ModKind `json:"kind"`
	// Numeric values in order of appearance.
	Values []float64 `json:"values"`
	// Trade stat id once matched (e.g. `explicit.stat_3299347043`).
	StatID   *string          `json:"stat_id"`
	Advanced *AdvancedModInfo `json:"advanced"`
}

// MarshalJSON writes serde-compatible JSON.
func (m Modifier) MarshalJSON() ([]byte, error) { return marshalStruct(m) }

// Property is a generic `Name: value` property in the properties block.
type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// `(augmented)` marker present.
	Augmented bool `json:"augmented"`
}

// MarshalJSON writes serde-compatible JSON.
func (p Property) MarshalJSON() ([]byte, error) { return marshalStruct(p) }

// Requirements are the level and attribute requirements.
type Requirements struct {
	Level *uint32 `json:"level"`
	Str   *uint32 `json:"str"`
	Dex   *uint32 `json:"dex"`
	Int   *uint32 `json:"int"`
}

// MarshalJSON writes serde-compatible JSON.
func (r Requirements) MarshalJSON() ([]byte, error) { return marshalStruct(r) }

// ItemStats are defence / offence numbers extracted from properties.
type ItemStats struct {
	Armour            *uint32  `json:"armour"`
	Evasion           *uint32  `json:"evasion"`
	EnergyShield      *uint32  `json:"energy_shield"`
	Spirit            *uint32  `json:"spirit"`
	BlockChance       *uint32  `json:"block_chance"`
	PhysicalDPS       *float64 `json:"physical_dps"`
	ElementalDPS      *float64 `json:"elemental_dps"`
	TotalDPS          *float64 `json:"total_dps"`
	AttacksPerSecond  *float64 `json:"attacks_per_second"`
	CriticalHitChance *float64 `json:"critical_hit_chance"`
}

// MarshalJSON writes serde-compatible JSON.
func (s ItemStats) MarshalJSON() ([]byte, error) { return marshalStruct(s) }

// ParsedItem is a fully parsed item. Every field is optional/empty unless present in the text.
type ParsedItem struct {
	Class    ItemClass    `json:"class"`
	Category ItemCategory `json:"category"`
	Rarity   Rarity       `json:"rarity"`
	// Unique / rare name line, if any (rares and uniques have two header lines).
	Name *string `json:"name"`
	// Base type line (e.g. `Expert Hunter Hood`, `Divine Orb`).
	BaseType     string     `json:"base_type"`
	ItemLevel    *uint32    `json:"item_level"`
	Quality      *uint32    `json:"quality"`
	StackSize    *StackSize `json:"stack_size"`
	GemLevel     *uint32    `json:"gem_level"`
	WaystoneTier *uint32    `json:"waystone_tier"`
	AreaLevel    *uint32    `json:"area_level"`
	// Number of rune/soul-core sockets (`Sockets: S S`).
	Sockets      uint8        `json:"sockets"`
	Requirements Requirements `json:"requirements"`
	Stats        ItemStats    `json:"stats"`
	Properties   []Property   `json:"properties"`
	Modifiers    []Modifier   `json:"modifiers"`
	Corrupted    bool         `json:"corrupted"`
	Unidentified bool         `json:"unidentified"`
	Mirrored     bool         `json:"mirrored"`
	Sanctified   bool         `json:"sanctified"`
	// `Note: ~price 1 divine` line if present.
	Note *string `json:"note"`
	// True if parsed from an advanced (Ctrl+Alt+C) copy.
	Advanced bool `json:"advanced"`
	// Original text, untouched.
	Raw string `json:"raw"`
}

// MarshalJSON writes serde-compatible JSON.
func (p ParsedItem) MarshalJSON() ([]byte, error) { return marshalStruct(p) }

// Clone returns a deep copy (Rust's `Clone`).
func (p *ParsedItem) Clone() *ParsedItem {
	out := *p
	out.Name = clonePtr(p.Name)
	out.ItemLevel = clonePtr(p.ItemLevel)
	out.Quality = clonePtr(p.Quality)
	out.StackSize = clonePtr(p.StackSize)
	out.GemLevel = clonePtr(p.GemLevel)
	out.WaystoneTier = clonePtr(p.WaystoneTier)
	out.AreaLevel = clonePtr(p.AreaLevel)
	out.Requirements = Requirements{
		Level: clonePtr(p.Requirements.Level), Str: clonePtr(p.Requirements.Str),
		Dex: clonePtr(p.Requirements.Dex), Int: clonePtr(p.Requirements.Int),
	}
	s := p.Stats
	out.Stats = ItemStats{
		Armour: clonePtr(s.Armour), Evasion: clonePtr(s.Evasion), EnergyShield: clonePtr(s.EnergyShield),
		Spirit: clonePtr(s.Spirit), BlockChance: clonePtr(s.BlockChance), PhysicalDPS: clonePtr(s.PhysicalDPS),
		ElementalDPS: clonePtr(s.ElementalDPS), TotalDPS: clonePtr(s.TotalDPS),
		AttacksPerSecond: clonePtr(s.AttacksPerSecond), CriticalHitChance: clonePtr(s.CriticalHitChance),
	}
	if p.Properties != nil {
		out.Properties = append([]Property{}, p.Properties...)
	}
	if p.Modifiers != nil {
		out.Modifiers = make([]Modifier, len(p.Modifiers))
		for i, m := range p.Modifiers {
			out.Modifiers[i] = m.clone()
		}
	}
	out.Note = clonePtr(p.Note)
	return &out
}

func (m Modifier) clone() Modifier {
	out := m
	if m.Values != nil {
		out.Values = append([]float64{}, m.Values...)
	}
	out.StatID = clonePtr(m.StatID)
	if m.Advanced != nil {
		a := m.Advanced.clone()
		out.Advanced = &a
	}
	return out
}

func (a AdvancedModInfo) clone() AdvancedModInfo {
	out := AdvancedModInfo{Affix: clonePtr(a.Affix), Name: clonePtr(a.Name), Tier: clonePtr(a.Tier)}
	if a.Ranges != nil {
		out.Ranges = append([]ValueRange{}, a.Ranges...)
	}
	return out
}

// StackSize is the `Stack Size: current/max` property.
type StackSize struct {
	Current uint32 `json:"current"`
	Max     uint32 `json:"max"`
}

// MarshalJSON writes serde-compatible JSON.
func (s StackSize) MarshalJSON() ([]byte, error) { return marshalStruct(s) }

// WeaponClasses are the weapon class headers as printed by PoE2 (plural form).
var WeaponClasses = []string{
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

// ItemClassFromHeader maps an `Item Class:` header value (e.g. "Stackable Currency") to an
// [ItemClass].
//
// Matching is case-insensitive and tolerant of singular/plural spellings. Unknown values
// are preserved in [ClassOther]; weapon subclasses keep their raw header in [ClassWeapon].
func ItemClassFromHeader(value string) ItemClass {
	raw := trimSpace(value)
	key := asciiLower(raw)
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
		for _, w := range WeaponClasses {
			if asciiEqualFold(w, raw) {
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

// TradeCategory is the trade2 `type_filters.category` option for this class, when one
// exists ("" and false otherwise).
//
// Ids follow `/api/trade2/data/filters` (`weapon.bow`, `armour.helmet`, ...).
func (c ItemClass) TradeCategory() (string, bool) {
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
		if asciiEqualFold(c.Value, "Meta Gems") {
			return "gem.metagem", true
		}
	}
	return "", false
}

// IsWeapon reports whether this class is a weapon.
func (c ItemClass) IsWeapon() bool { return c.Kind == ClassWeapon }

func weaponTradeCategory(raw string) (string, bool) {
	key := asciiLower(raw)
	for len(key) > 0 && key[len(key)-1] == 's' {
		key = key[:len(key)-1]
	}
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
	for _, w := range WeaponClasses {
		w = asciiLower(w)
		var singular string
		if s, ok := cutSuffix(w, "ves"); ok {
			singular = s + "ff"
		} else if s, ok := cutSuffix(w, "s"); ok {
			singular = s
		}
		if singular == key {
			return true
		}
	}
	return false
}

// RarityFromHeader parses the value of a `Rarity:` line.
func RarityFromHeader(value string) (Rarity, bool) {
	switch asciiLower(trimSpace(value)) {
	case "normal":
		return RarityNormal, true
	case "magic":
		return RarityMagic, true
	case "rare":
		return RarityRare, true
	case "unique":
		return RarityUnique, true
	case "currency":
		return RarityCurrency, true
	case "gem":
		return RarityGem, true
	case "quest", "divination card", "other":
		return RarityOther, true
	}
	return "", false
}

// TradeOption is the lower-case trade2 rarity option (`normal`, `magic`, `rare`, `unique`),
// if any.
func (r Rarity) TradeOption() (string, bool) {
	switch r {
	case RarityNormal, RarityMagic, RarityRare, RarityUnique:
		return string(r), true
	}
	return "", false
}
