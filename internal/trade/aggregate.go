package trade

// Helpers shared by the aggregated price sources (poe.ninja, poe2scout), which report one
// price per item instead of listings.

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// displayPrice picks a readable currency for amount (expressed in rates.Base): divine when
// it is worth at least one divine, otherwise exalted; falls back to the base currency.
func displayPrice(amount float64, rates *core.ExchangeRates) core.Price {
	if div, ok := rates.Rate("divine"); ok {
		if inDiv := amount / div; inDiv >= 1 {
			return core.Price{Amount: inDiv, Currency: "divine"}
		}
	}
	if ex, ok := rates.Rate("exalted"); ok {
		return core.Price{Amount: amount / ex, Currency: "exalted"}
	}
	return core.Price{Amount: amount, Currency: rates.Base}
}

// aggregatedSummary is the single-value summary for an aggregated source (no individual
// listings).
func aggregatedSummary(source core.PriceSourceKind, price core.Price, total uint32) core.PriceSummary {
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

// exchangeName is the name an aggregated source lists this item under: uncut gems carry
// their level (`Uncut Support Gem (Level 2)`), everything else uses the base type.
func exchangeName(item *core.ParsedItem) string {
	if item.Category == core.CategoryGem && item.GemLevel != nil && strings.HasPrefix(item.BaseType, "Uncut ") {
		return fmt.Sprintf("%s (Level %d)", item.BaseType, *item.GemLevel)
	}
	return item.BaseType
}

// DefaultSourceCacheTTL is how long the aggregated sources cache responses in production:
// they refresh roughly hourly, and ten minutes keeps RKL well inside their "don't hammer the
// economy endpoints" policies.
const DefaultSourceCacheTTL = 10 * time.Minute

// SourceOptions configure an aggregated price source.
type SourceOptions struct {
	// BaseURL is the API root; a mock server in tests.
	BaseURL *url.URL
	// UserAgent is sent with every request.
	UserAgent string
	// CacheTTL is how long responses are cached; 0 disables the cache.
	CacheTTL time.Duration
}
