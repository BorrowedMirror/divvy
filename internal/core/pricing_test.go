package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func listing(amount float64, currency string) Listing {
	return Listing{
		ID:     formatNumber(amount) + currency,
		Price:  Price{Amount: amount, Currency: currency},
		Online: true,
	}
}

func pp(amount float64, currency string) *Price { return &Price{Amount: amount, Currency: currency} }

func TestSummaryEmpty(t *testing.T) {
	s := Summarise(SourceTrade, 0, nil, nil)
	assert.Nil(t, s.Min)
	assert.Nil(t, s.Median)
	assert.Nil(t, s.Max)
	assert.Equal(t, uint32(0), s.Total)
}

func TestSingleCurrencyWithOutliers(t *testing.T) {
	var ls []Listing
	for _, a := range []float64{1, 10, 11, 12, 12, 13, 500} {
		ls = append(ls, listing(a, "exalted"))
	}
	s := Summarise(SourceTrade, 120, ls, nil)
	assert.Equal(t, pp(10, "exalted"), s.Min)
	assert.Equal(t, pp(12, "exalted"), s.Median)
	assert.Equal(t, pp(13, "exalted"), s.Max)
	assert.Len(t, s.Listings, 7)
	assert.Equal(t, uint32(120), s.Total)
	assert.Equal(t, SourceTrade, s.Source)
}

func TestEvenMedianAndSmallSample(t *testing.T) {
	ls := []Listing{listing(1, "divine"), listing(2, "divine"), listing(100, "divine")}
	s := Summarise(SourcePoeNinja, 3, ls, nil)
	assert.Equal(t, pp(100, "divine"), s.Max, "no trimming below threshold")
	ls = []Listing{listing(1, "divine"), listing(2, "divine")}
	assert.Equal(t, pp(1.5, "divine"), Summarise(SourcePoe2Scout, 2, ls, nil).Median)
}

func rate(r *ExchangeRates, c string) *float64 {
	if v, ok := r.Rate(c); ok {
		return &v
	}
	return nil
}

func convert(r *ExchangeRates, a float64, from, to string) *float64 {
	if v, ok := r.Convert(a, from, to); ok {
		return &v
	}
	return nil
}

func TestConvertsWithRates(t *testing.T) {
	rates := &ExchangeRates{Base: "exalted", Rates: map[string]float64{"divine": 200, "chaos": 10, "bad": 0}}
	assert.Equal(t, new(1.0), rate(rates, "exalted"))
	assert.Nil(t, rate(rates, "bad"))
	assert.Equal(t, new(20.0), convert(rates, 1, "divine", "chaos"))
	assert.Nil(t, convert(rates, 1, "bad", "chaos"))
	ls := func() []Listing {
		return []Listing{
			listing(1, "divine"),
			listing(2, "divine"),
			listing(100, "exalted"),
			listing(5, "mirror"),
			listing(-1, "divine"),
		}
	}
	s := Summarise(SourceTrade, 5, ls(), rates)
	assert.Equal(t, pp(0.5, "divine"), s.Min)
	assert.Equal(t, pp(2, "divine"), s.Max)
	// Without rates, foreign currencies are ignored.
	s = Summarise(SourceTrade, 5, ls(), nil)
	assert.Equal(t, pp(1, "divine"), s.Min)
	// Nothing priceable.
	s = Summarise(SourceTrade, 1, []Listing{listing(0, "divine")}, nil)
	assert.Nil(t, s.Min)
}

func TestTieBreaksAlphabetically(t *testing.T) {
	ls := []Listing{listing(3, "exalted"), listing(1, "divine")}
	assert.Equal(t, pp(1, "divine"), Summarise(SourceTrade, 2, ls, nil).Min)
}

func TestSummaryIsOrdered(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		amounts := rapid.SliceOfN(rapid.Float64Range(0.01, 10_000), 1, 39).Draw(t, "amounts")
		var ls []Listing
		for _, a := range amounts {
			ls = append(ls, listing(a, "exalted"))
		}
		s := Summarise(SourceTrade, 0, ls, nil)
		if !(s.Min.Amount <= s.Median.Amount && s.Median.Amount <= s.Max.Amount) {
			t.Fatalf("unordered: %v %v %v", s.Min.Amount, s.Median.Amount, s.Max.Amount)
		}
	})
}

func TestEquivalentsAndRatesInSummaries(t *testing.T) {
	rates := &ExchangeRates{Base: "exalted", Rates: map[string]float64{"divine": 200, "chaos": 10}}
	assert.Equal(t, pp(2, "divine"), equivalent(pp(40, "chaos"), rates))
	assert.Equal(t, pp(50, "exalted"), equivalent(pp(5, "chaos"), rates))
	assert.Nil(t, equivalent(pp(30, "exalted"), rates), "already exalted")
	assert.Nil(t, equivalent(pp(2, "divine"), rates), "already divine")
	assert.Nil(t, equivalent(pp(1, "mirror"), rates), "unknown rate")
	s := Summarise(SourceTrade, 3, []Listing{listing(1, "chaos"), listing(2, "chaos"), listing(20, "exalted")}, nil)
	assert.Equal(t, pp(2, "chaos"), s.Max, "without rates the exalted listing is left out")
	s.TradeURL = new("u")
	s = WithRates(s, rates)
	assert.Equal(t, pp(2, "chaos"), s.Max)
	assert.Nil(t, s.Listings[2].Equivalent, "20 ex is under a divine and already exalted")
	assert.Equal(t, pp(10, "exalted"), s.Listings[0].Equivalent)
	assert.Equal(t, new("u"), s.TradeURL)
}

func TestSummariseAndWithRatesKeepTheirInputs(t *testing.T) {
	rates := &ExchangeRates{Base: "exalted", Rates: map[string]float64{"divine": 200}}
	in := []Listing{listing(300, "exalted"), listing(1, "divine")}
	s := Summarise(SourceTrade, 2, in, rates)
	assert.Nil(t, in[0].Equivalent, "input listings are not changed")
	assert.Equal(t, pp(1.5, "divine"), s.Listings[0].Equivalent)

	plain := Summarise(SourceTrade, 2, in, nil)
	plain.TradeURL = new("https://example.invalid")
	converted := WithRates(plain, rates)
	assert.Nil(t, plain.Listings[0].Equivalent, "the summary passed in is not changed")
	assert.Equal(t, plain.TradeURL, converted.TradeURL)
	assert.Equal(t, pp(1.5, "divine"), converted.Listings[0].Equivalent)
}

func TestPricingJSON(t *testing.T) {
	l := Listing{ID: "x", Price: Price{Amount: 2, Currency: "divine"}, Online: true, ItemLevel: new(uint32(80))}
	assert.JSONEq(t, `{"id":"x","price":{"amount":2,"currency":"divine"},"seller":null,"online":true,"indexed":null,"item_level":80,"stack_size":null,"whisper":null,"equivalent":null}`, marshal(t, l))
	r := ExchangeRates{Base: "exalted", Rates: map[string]float64{"divine": 200, "annul": 0.5}}
	assert.JSONEq(t, `{"base":"exalted","rates":{"annul":0.5,"divine":200}}`, marshal(t, r))
	assert.JSONEq(t, `{"source":"trade","total":0,"listings":[],"min":null,"median":null,"max":null,"trade_url":null}`,
		marshal(t, Summarise(SourceTrade, 0, nil, nil)))
	assert.NotNil(t, Summarise(SourceTrade, 0, nil, nil).Listings, "listings are never nil")
}
