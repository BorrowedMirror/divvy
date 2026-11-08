package core

import "github.com/BorrowedMirror/rust-king-ledger/internal/enum"

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

// UnmarshalText accepts only the known kinds.
func (k *ModKind) UnmarshalText(text []byte) error {
	return enum.Parse(k, text, ModImplicit, ModExplicit, ModEnchant, ModRune, ModCrafted, ModFractured,
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

// Affix is the kind of an advanced mod header.
type Affix string

// Affix values.
const (
	AffixPrefix   Affix = "prefix"
	AffixSuffix   Affix = "suffix"
	AffixImplicit Affix = "implicit"
)

// UnmarshalText accepts only the known affixes.
func (a *Affix) UnmarshalText(text []byte) error {
	return enum.Parse(a, text, AffixPrefix, AffixSuffix, AffixImplicit)
}

// ValueRange is a printed roll range.
type ValueRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

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

// Property is a generic `Name: value` property in the properties block.
type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// `(augmented)` marker present.
	Augmented bool `json:"augmented"`
}

// Requirements are the level and attribute requirements.
type Requirements struct {
	Level *uint32 `json:"level"`
	Str   *uint32 `json:"str"`
	Dex   *uint32 `json:"dex"`
	Int   *uint32 `json:"int"`
}

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

// StackSize is the `Stack Size: current/max` property.
type StackSize struct {
	Current uint32 `json:"current"`
	Max     uint32 `json:"max"`
}
