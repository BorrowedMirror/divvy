package trade

// Exchange-rate helpers shared by the aggregated price sources.
//
// [core.ExchangeRates] semantics follow core: Rates[c] = value of one c in Base.

import (
	"fmt"
	"math"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// ValueInBase is the value of one currency in rates.Base, if known.
func ValueInBase(rates *core.ExchangeRates, currency string) (float64, bool) {
	if currency == rates.Base {
		return 1, true
	}
	v, ok := rates.Rates[currency]
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false
	}
	return v, true
}

// DisplayPrice picks a readable currency for amount (expressed in rates.Base): divine when
// it is worth at least one divine, otherwise exalted; falls back to the base currency.
func DisplayPrice(amount float64, rates *core.ExchangeRates) core.Price {
	if div, ok := ValueInBase(rates, "divine"); ok {
		if inDiv := amount / div; inDiv >= 1 {
			return core.Price{Amount: inDiv, Currency: "divine"}
		}
	}
	if ex, ok := ValueInBase(rates, "exalted"); ok {
		return core.Price{Amount: amount / ex, Currency: "exalted"}
	}
	return core.Price{Amount: amount, Currency: rates.Base}
}

// AggregatedSummary is the single-value summary for an aggregated source (no individual
// listings).
func AggregatedSummary(source core.PriceSourceKind, price core.Price, total uint32) core.PriceSummary {
	minP, median, maxP := price, price, price
	return core.PriceSummary{
		Source:   source,
		Total:    total,
		Listings: []core.Listing{},
		Min:      &minP,
		Median:   &median,
		Max:      &maxP,
	}
}

// ExchangeName is the name an aggregated source lists this item under: uncut gems carry
// their level (`Uncut Support Gem (Level 2)`), everything else uses the base type.
func ExchangeName(item *core.ParsedItem) string {
	if item.Category == core.CategoryGem && item.GemLevel != nil && strings.HasPrefix(item.BaseType, "Uncut ") {
		return fmt.Sprintf("%s (Level %d)", item.BaseType, *item.GemLevel)
	}
	return item.BaseType
}
