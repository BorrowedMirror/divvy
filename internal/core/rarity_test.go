package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func rarityOf(s string) *Rarity {
	if r, ok := rarityFromHeader(s); ok {
		return &r
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
}
