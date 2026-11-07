package core

// Price aggregation over listings.

import (
	"cmp"
	"math"
	"slices"
)

// Price is an amount in a trade currency.
type Price struct {
	Amount float64 `json:"amount"`
	// Trade currency id, e.g. `divine`, `exalted`, `chaos`.
	Currency string `json:"currency"`
}

// MarshalJSON writes serde-compatible JSON.
func (p Price) MarshalJSON() ([]byte, error) { return marshalStruct(p) }

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

// MarshalJSON writes serde-compatible JSON.
func (l Listing) MarshalJSON() ([]byte, error) { return marshalStruct(l) }

// PriceSourceKind says where a price came from.
type PriceSourceKind string

// PriceSourceKind values.
const (
	SourceTrade     PriceSourceKind = "trade"
	SourcePoeNinja  PriceSourceKind = "poe_ninja"
	SourcePoe2Scout PriceSourceKind = "poe2_scout"
)

// UnmarshalJSON accepts only the known variants, like serde.
func (k *PriceSourceKind) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, k, "PriceSourceKind", SourceTrade, SourcePoeNinja, SourcePoe2Scout)
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

// MarshalJSON writes serde-compatible JSON.
func (s PriceSummary) MarshalJSON() ([]byte, error) { return marshalStruct(s) }

// ExchangeRates normalise prices into one currency; Rates[c] = value of 1 c in Base.
type ExchangeRates struct {
	Base  string             `json:"base"`
	Rates map[string]float64 `json:"rates"`
}

// MarshalJSON writes serde-compatible JSON (rates sorted by key, like a BTreeMap).
func (r ExchangeRates) MarshalJSON() ([]byte, error) { return marshalStruct(r) }

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

// TrimMinListings is the minimum number of priced listings before outlier trimming kicks in.
const TrimMinListings = 4

// iqrFence is the Tukey fence multiplier for outlier trimming.
const iqrFence = 1.5

func quantile(sorted []float64, q float64) float64 {
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

// TrimOutliers drops values outside the Tukey fences (`Q1 - 1.5 IQR`, `Q3 + 1.5 IQR`).
//
// This removes both price-fixing bait at the low end and absurd asks at the high end
// while keeping small samples intact.
func TrimOutliers(sorted []float64) []float64 {
	if len(sorted) < TrimMinListings {
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

// totalCmp is `f64::total_cmp`.
func totalCmp(a, b float64) int {
	key := func(f float64) int64 {
		bits := int64(math.Float64bits(f))
		return bits ^ int64(uint64(bits>>63)>>1)
	}
	return cmp.Compare(key(a), key(b))
}

// midpoint is `f64::midpoint`.
func midpoint(a, b float64) float64 {
	const lo = 2.2250738585072014e-308 * 2 // f64::MIN_POSITIVE * 2
	const hi = math.MaxFloat64 / 2
	absA, absB := math.Abs(a), math.Abs(b)
	switch {
	case absA <= hi && absB <= hi:
		return (a + b) / 2
	case absA < lo:
		return a + b/2
	case absB < lo:
		return a/2 + b
	}
	return a/2 + b/2
}

// Summarise aggregates listings into a summary. Pure; outlier trimming lives here.
//
// Prices are normalised to the most common currency among the listings (ties broken
// alphabetically). With rates, other currencies are converted; without (nil), listings in
// other currencies are left out of min/median/max (but stay in Listings). Stack prices are
// per listing as given by the source. Listings is taken over (and modified) like the Rust
// by-value argument.
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
		slices.SortStableFunc(values, totalCmp)
		kept := TrimOutliers(values)
		price := func(amount float64) *Price { return &Price{Amount: round2(amount), Currency: target} }
		if len(kept) > 0 {
			mid := len(kept) / 2
			var median float64
			if len(kept)%2 == 0 {
				median = midpoint(kept[mid-1], kept[mid])
			} else {
				median = kept[mid]
			}
			minP, medianP, maxP = price(kept[0]), price(median), price(kept[len(kept)-1])
		}
	}

	if rates != nil {
		for i := range listings {
			listings[i].Equivalent = Equivalent(&listings[i].Price, rates)
		}
	}
	if listings == nil {
		listings = []Listing{}
	}
	return PriceSummary{Source: source, Total: total, Listings: listings, Min: minP, Median: medianP, Max: maxP}
}

// WithRates recomputes a summary's figures with exchange rates (listings in other
// currencies then count towards min/median/max, and each listing gets its
// [Listing.Equivalent]).
func WithRates(summary PriceSummary, rates *ExchangeRates) PriceSummary {
	tradeURL := clonePtr(summary.TradeURL)
	out := Summarise(summary.Source, summary.Total, summary.Listings, rates)
	out.TradeURL = tradeURL
	return out
}

// Equivalent is price in divine when worth at least one divine, otherwise in exalted; nil
// when the price is already in that currency or a rate is unknown.
func Equivalent(price *Price, rates *ExchangeRates) *Price {
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
