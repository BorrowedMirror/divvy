package trade

import (
	"math"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/stretchr/testify/assert"
)

func strp(s string) *string { return &s }

// testItem is the minimal parsed item for source tests (Rust `test_support::item`).
func testItem(class core.ItemClass, category core.ItemCategory, name *string, base string) core.ParsedItem {
	rarity := core.RarityCurrency
	if category == core.CategoryUnique {
		rarity = core.RarityUnique
	}
	return core.ParsedItem{Class: class, Category: category, Rarity: rarity, Name: name, BaseType: base}
}

func ninjaRates() core.ExchangeRates {
	return core.ExchangeRates{Base: "divine", Rates: map[string]float64{"exalted": 1.0 / 724.8, "chaos": 1.0 / 9.36}}
}

func TestDisplaysInDivineOrExalted(t *testing.T) {
	r := ninjaRates()
	assert.Equal(t, core.Price{Amount: 130, Currency: "divine"}, DisplayPrice(130, &r))
	p := DisplayPrice(0.5, &r)
	assert.Equal(t, "exalted", p.Currency)
	assert.InDelta(t, 362.4, p.Amount, 1e-6)
	scout := core.ExchangeRates{Base: "exalted", Rates: map[string]float64{"divine": 966}}
	assert.Equal(t, core.Price{Amount: 15, Currency: "exalted"}, DisplayPrice(15, &scout))
	assert.Equal(t, core.Price{Amount: 2, Currency: "divine"}, DisplayPrice(1932, &scout))
	bare := core.ExchangeRates{Base: "chaos", Rates: map[string]float64{"divine": math.NaN()}}
	assert.Equal(t, core.Price{Amount: 3, Currency: "chaos"}, DisplayPrice(3, &bare))
}

func TestSummaryHasSingleValue(t *testing.T) {
	s := AggregatedSummary(core.SourcePoeNinja, core.Price{Amount: 1, Currency: "divine"}, 7)
	assert.Equal(t, uint32(7), s.Total)
	assert.Equal(t, s.Min, s.Max)
	assert.Empty(t, s.Listings)
	assert.Nil(t, s.TradeURL)
}

func TestExchangeNameForUncutGems(t *testing.T) {
	gem := testItem(core.ClassUncutGem.Class(), core.CategoryGem, nil, "Uncut Support Gem")
	assert.Equal(t, "Uncut Support Gem", ExchangeName(&gem))
	two := uint32(2)
	gem.GemLevel = &two
	assert.Equal(t, "Uncut Support Gem (Level 2)", ExchangeName(&gem))
	orb := testItem(core.ClassStackableCurrency.Class(), core.CategoryCurrency, nil, "Divine Orb")
	assert.Equal(t, "Divine Orb", ExchangeName(&orb))
}
