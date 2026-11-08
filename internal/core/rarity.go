package core

import (
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/enum"
)

// Rarity is the rarity line (`Rarity: ...`) of an item.
type Rarity string

// Rarity values.
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

// UnmarshalText accepts only the known rarities.
func (r *Rarity) UnmarshalText(text []byte) error {
	return enum.Parse(r, text, RarityNormal, RarityMagic, RarityRare, RarityUnique, RarityCurrency, RarityGem, RarityOther)
}

// rarityFromHeader parses the value of a `Rarity:` line.
func rarityFromHeader(value string) (Rarity, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
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
