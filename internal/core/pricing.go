package core

// Price aggregation over listings.

import (
	"math"
	"slices"

	"github.com/BorrowedMirror/divvy/internal/enum"
)

// Price is an amount in a trade currency.
type Price struct {
	Amount float64 `json:"amount"`
	// Trade currency id, e.g. `divine`, `exalted`, `chaos`.
	Currency string `json:"currency"`
}

// Listing is one listing from a price source.
type Listing struct {
	ID     string  `json:"id"`
	Price  Price   `json:"price"`
	Seller *string `json:"seller"`
	Online bool    `json:"online"`
	// RFC 3339 listing timestamp.
	Indexed   *string `json:"indexed"`
	ItemLevel *uint32 `json:"item_level"`
	StackSize *uint32 `json:"stack_size"`
	// Trade whisper to paste into chat (trade site listings only; none for instant buyout).
	Whisper *string `json:"whisper"`
	// The price in divine (worth one or more) or exalted, when it is in another currency and
	// exchange rates are known.
	Equivalent *Price `json:"equivalent"`
}

// PriceSourceKind says where a price came from.
type PriceSourceKind string

// PriceSourceKind values.
const (
	SourceTrade     PriceSourceKind = "trade"
	SourcePoeNinja  PriceSourceKind = "poe_ninja"
	SourcePoe2Scout PriceSourceKind = "poe2_scout"
)

// UnmarshalText accepts only the known sources.
func (k *PriceSourceKind) UnmarshalText(text []byte) error {
	return enum.Parse(k, text, SourceTrade, SourcePoeNinja, SourcePoe2Scout)
}

// PriceSummary is the aggregated result displayed by the overlay.
type PriceSummary struct {
	Source PriceSourceKind `json:"source"`
	// Total matches reported by the source (may exceed len(Listings)).
	Total    uint32    `json:"total"`
	Listings []Listing `json:"listings"`
	// Min / median / max normalised to the most common currency among listings.
	Min    *Price `json:"min"`
	Median *Price `json:"median"`
	Max    *Price `json:"max"`
	// Link that opens the same search on pathofexile.com.
	TradeURL *string `json:"trade_url"`
}

// ExchangeRates normalise prices into one currency; Rates[c] = value of 1 c in Base.
type ExchangeRates struct {
	Base  string             `json:"base"`
	Rates map[string]float64 `json:"rates"`
}

// Rate is the value of one currency in Base (Base itself is 1).
func (r *ExchangeRates) Rate(currency string) (float64, bool) {
	if currency == r.Base {
		return 1, true
	}
	v, ok := r.Rates[currency]
	if !ok || math.IsInf(v, 0) || math.IsNaN(v) || v <= 0 {
		return 0, false
	}
	return v, true
}

// Convert converts amount of from into to, when both rates are known.
func (r *ExchangeRates) Convert(amount float64, from, to string) (float64, bool) {
	if from == to {
		return amount, true
	}
	rf, ok := r.Rate(from)
	if !ok {
		return 0, false
	}
	rt, ok := r.Rate(to)
	if !ok {
		return 0, false
	}
	return amount * rf / rt, true
}

// trimMinListings is the minimum number of priced listings before outlier trimming kicks in.
const trimMinListings = 4

// iqrFence is the Tukey fence multiplier for outlier trimming.
const iqrFence = 1.5

func quantile(sorted []float64, q float64) float64 {
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

// trimOutliers drops values outside the Tukey fences (`Q1 - 1.5 IQR`, `Q3 + 1.5 IQR`).
//
// This removes both price-fixing bait at the low end and absurd asks at the high end
// while keeping small samples intact.
func trimOutliers(sorted []float64) []float64 {
	if len(sorted) < trimMinListings {
		return slices.Clone(sorted)
	}
	q1 := quantile(sorted, 0.25)
	q3 := quantile(sorted, 0.75)
	iqr := q3 - q1
	lo, hi := q1-iqrFence*iqr, q3+iqrFence*iqr
	out := []float64{}
	for _, v := range sorted {
		if lo <= v && v <= hi {
			out = append(out, v)
		}
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func isFinite(v float64) bool { return !math.IsInf(v, 0) && !math.IsNaN(v) }

// Summarise aggregates listings into a summary. Pure; outlier trimming lives here.
//
// Prices are normalised to the most common currency among the listings (ties broken
// alphabetically). With rates, other currencies are converted; without (nil), listings in
// other currencies are left out of min/median/max (but stay in Listings). Stack prices are
// per listing as given by the source. The summary holds a copy of listings; with rates each
// copy gets its [Listing.Equivalent].
func Summarise(source PriceSourceKind, total uint32, listings []Listing, rates *ExchangeRates) PriceSummary {
	counts := map[string]int{}
	for _, l := range listings {
		counts[l.Price.Currency]++
	}
	target, found := "", false
	best := -1
	for c, n := range counts {
		if n > best || (n == best && c < target) {
			target, best, found = c, n, true
		}
	}

	var minP, medianP, maxP *Price
	if found {
		var values []float64
		for _, l := range listings {
			if !isFinite(l.Price.Amount) || l.Price.Amount <= 0 {
				continue
			}
			if l.Price.Currency == target {
				values = append(values, l.Price.Amount)
			} else if rates != nil {
				if v, ok := rates.Convert(l.Price.Amount, l.Price.Currency, target); ok {
					values = append(values, v)
				}
			}
		}
		slices.Sort(values)
		kept := trimOutliers(values)
		price := func(amount float64) *Price { return &Price{Amount: round2(amount), Currency: target} }
		if len(kept) > 0 {
			mid := len(kept) / 2
			var median float64
			if len(kept)%2 == 0 {
				median = (kept[mid-1] + kept[mid]) / 2
			} else {
				median = kept[mid]
			}
			minP, medianP, maxP = price(kept[0]), price(median), price(kept[len(kept)-1])
		}
	}

	out := make([]Listing, len(listings))
	copy(out, listings)
	if rates != nil {
		for i := range out {
			out[i].Equivalent = equivalent(&out[i].Price, rates)
		}
	}
	return PriceSummary{Source: source, Total: total, Listings: out, Min: minP, Median: medianP, Max: maxP}
}

// WithRates recomputes a summary's figures with exchange rates (listings in other
// currencies then count towards min/median/max, and each listing gets its
// [Listing.Equivalent]). The summary passed in is not changed.
func WithRates(summary PriceSummary, rates *ExchangeRates) PriceSummary {
	out := Summarise(summary.Source, summary.Total, summary.Listings, rates)
	out.TradeURL = summary.TradeURL
	return out
}

// equivalent is price in divine when worth at least one divine, otherwise in exalted; nil
// when the price is already in that currency or a rate is unknown.
func equivalent(price *Price, rates *ExchangeRates) *Price {
	if !isFinite(price.Amount) || price.Amount <= 0 {
		return nil
	}
	div, ok := rates.Convert(price.Amount, price.Currency, "divine")
	if !ok {
		return nil
	}
	target := "exalted"
	if _, hasEx := rates.Rate("exalted"); div >= 1 || !hasEx {
		target = "divine"
	}
	if target == price.Currency {
		return nil
	}
	amount := div
	if target != "divine" {
		if amount, ok = rates.Convert(price.Amount, price.Currency, "exalted"); !ok {
			return nil
		}
	}
	return &Price{Amount: round2(amount), Currency: target}
}
