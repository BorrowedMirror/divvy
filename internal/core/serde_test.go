package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serde(t *testing.T, v any) string {
	t.Helper()
	b, err := MarshalSerde(v)
	require.NoError(t, err)
	return string(b)
}

// Expected strings were produced by serde_json 1.0.154 (the version in Cargo.lock).
func TestF64MatchesSerdeJSON(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0, "0.0"},
		{math.Copysign(0, -1), "-0.0"},
		{1, "1.0"},
		{40, "40.0"},
		{0.1, "0.1"},
		{1e15, "1000000000000000.0"},
		{1e16, "1e+16"},
		{1.5e16, "1.5e+16"},
		{123456789012345678, "1.2345678901234568e+17"},
		{1e-5, "0.00001"},
		{1e-6, "1e-6"},
		{1.25e-7, "1.25e-7"},
		{1e300, "1e+300"},
		{5e-324, "5e-324"},
		{math.MaxFloat64, "1.7976931348623157e+308"},
		{161.15, "161.15"},
		{add(0.3, 0.6), "0.8999999999999999"}, // runtime sum, not a constant
		{-2.5, "-2.5"},
		{math.NaN(), "null"},
		{math.Inf(1), "null"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, serde(t, c.v), "%v", c.v)
	}
}

func TestStringEscapingMatchesSerdeJSON(t *testing.T) {
	assert.Equal(t, "\"a<b>& \u007f\\u0001\\t\\\"\"", serde(t, "a<b>& \u007f\u0001\t\""))
	// Encoding through encoding/json keeps the bytes when HTML escaping is off.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	require.NoError(t, enc.Encode(Property{Name: "<a&b>", Value: "1.0"}))
	assert.Equal(t, `{"name":"<a&b>","value":"1.0","augmented":false}`+"\n", buf.String())
}

func TestItemClassJSON(t *testing.T) {
	assert.Equal(t, `{"type":"helmet"}`, serde(t, ClassHelmet.Class()))
	assert.Equal(t, `{"type":"body_armour"}`, serde(t, ClassBodyArmour.Class()))
	assert.Equal(t, `{"type":"weapon","value":"Bows"}`, serde(t, WeaponClass("Bows")))
	assert.Equal(t, `{"type":"other","value":"Meta Gems"}`, serde(t, OtherClass("Meta Gems")))
	for _, c := range []ItemClass{ClassSoulCore.Class(), WeaponClass("Bows"), OtherClass("x")} {
		var back ItemClass
		require.NoError(t, json.Unmarshal([]byte(serde(t, c)), &back))
		assert.Equal(t, c, back)
	}
	var c ItemClass
	assert.Error(t, json.Unmarshal([]byte(`{"type":"lance"}`), &c))
	assert.Error(t, json.Unmarshal([]byte(`{"type":"weapon"}`), &c))
	assert.Equal(t, `Weapon("Bows")`, WeaponClass("Bows").String())
	assert.Equal(t, "SoulCore", ClassSoulCore.Class().String())
}

func TestUnitEnumsRejectUnknownVariants(t *testing.T) {
	var r Rarity
	require.NoError(t, json.Unmarshal([]byte(`"rare"`), &r))
	assert.Equal(t, RarityRare, r)
	assert.Error(t, json.Unmarshal([]byte(`"Rare"`), &r))
	var k ModKind
	assert.Error(t, json.Unmarshal([]byte(`"bogus"`), &k))
	var s PriceSourceKind
	require.NoError(t, json.Unmarshal([]byte(`"poe2_scout"`), &s))
	assert.Equal(t, SourcePoe2Scout, s)
	assert.Equal(t, `"poe_ninja"`, serde(t, SourcePoeNinja))
}

func TestErrorJSON(t *testing.T) {
	cases := map[string]ParseError{
		`{"kind":"empty"}`:                                                      {Kind: ParseErrEmpty},
		`{"kind":"not_an_item"}`:                                                {Kind: ParseErrNotAnItem},
		`{"kind":"unknown_rarity","detail":"Legendary"}`:                        {Kind: ParseErrUnknownRarity, Value: "Legendary"},
		`{"kind":"unknown_item_class","detail":"X"}`:                            {Kind: ParseErrUnknownItemClass, Value: "X"},
		`{"kind":"malformed","detail":{"section":"header","reason":"missing"}}`: {Kind: ParseErrMalformed, Section: "header", Reason: "missing"},
	}
	for want, e := range cases {
		assert.Equal(t, want, serde(t, e))
		var back ParseError
		require.NoError(t, json.Unmarshal([]byte(want), &back))
		assert.Equal(t, e, back)
	}
	assert.Equal(t, "unknown rarity `Legendary`", ParseError{Kind: ParseErrUnknownRarity, Value: "Legendary"}.Error())
	assert.Equal(t, "malformed section `header`: missing item name",
		ParseError{Kind: ParseErrMalformed, Section: "header", Reason: "missing item name"}.Error())

	ce := CoreErrorFromParse(ParseError{Kind: ParseErrEmpty})
	assert.Equal(t, `{"kind":"parse","detail":{"kind":"empty"}}`, serde(t, ce))
	assert.Equal(t, "item text is empty", ce.Error())
	var pe ParseError
	assert.True(t, errors.As(ce, &pe))
	un := CoreError{Kind: CoreErrUnpriceable, Message: "maps"}
	assert.Equal(t, `{"kind":"unpriceable","detail":"maps"}`, serde(t, un))
	assert.Equal(t, "item cannot be priced: maps", un.Error())
	for _, e := range []CoreError{ce, un} {
		var back CoreError
		require.NoError(t, json.Unmarshal([]byte(serde(t, e)), &back))
		assert.Equal(t, e, back)
	}
}

func TestVecsAreNeverNullAndBytesAreNumbers(t *testing.T) {
	f := StatFilter{StatID: "a", Text: "b", Tiers: []uint8{3, 7}}
	assert.Equal(t, `{"stat_id":"a","text":"b","enabled":false,"min":null,"max":null,"item_values":[],"tiers":[3,7]}`, serde(t, f))
	var back StatFilter
	require.NoError(t, json.Unmarshal([]byte(serde(t, f)), &back))
	assert.Equal(t, []uint8{3, 7}, back.Tiers)
	// `#[serde(default)]` fields may be missing.
	require.NoError(t, json.Unmarshal([]byte(`{"stat_id":"a","text":"b","enabled":true,"min":1.5,"max":null,"item_values":[1.5]}`), &back))
	assert.Equal(t, ptr(1.5), back.Min)
	assert.Nil(t, back.Tiers)
	assert.Equal(t, `{"id":"i","text":"t","kind":"explicit","aliases":[]}`, serde(t, StatDef{ID: "i", Text: "t", Kind: "explicit"}))
}

func TestPricingJSON(t *testing.T) {
	l := Listing{ID: "x", Price: Price{Amount: 2, Currency: "divine"}, Online: true, ItemLevel: ptr(uint32(80))}
	assert.Equal(t, `{"id":"x","price":{"amount":2.0,"currency":"divine"},"seller":null,"online":true,"indexed":null,"item_level":80,"stack_size":null,"whisper":null,"equivalent":null}`, serde(t, l))
	r := ExchangeRates{Base: "exalted", Rates: map[string]float64{"divine": 200, "chaos": 10, "annul": 0.5}}
	assert.Equal(t, `{"base":"exalted","rates":{"annul":0.5,"chaos":10.0,"divine":200.0}}`, serde(t, r))
	assert.Equal(t, `{"base":"","rates":{}}`, serde(t, ExchangeRates{}))
	s := Summarise(SourceTrade, 3, []Listing{l}, &r)
	b := serde(t, s)
	var back PriceSummary
	require.NoError(t, json.Unmarshal([]byte(b), &back))
	assert.Equal(t, b, serde(t, back))
	assert.Equal(t, `{"source":"trade","total":0,"listings":[],"min":null,"median":null,"max":null,"trade_url":null}`,
		serde(t, Summarise(SourceTrade, 0, nil, nil)))
}

func TestLanguageAndOptionsJSON(t *testing.T) {
	lang := Language{Name: "German", Code: "de", Keys: pairs("Rarity", "Seltenheit")}
	want := `{"name":"German","code":"de","keys":[["Rarity","Seltenheit"]],"rarities":[],"flags":[],"words":[],"patterns":[],"classes":[],"bases":[],"uniques":[],"stats":[]}`
	assert.Equal(t, want, serde(t, lang))
	var back Language
	require.NoError(t, json.Unmarshal([]byte(want), &back))
	assert.Equal(t, lang.Keys, back.Keys)
	assert.Equal(t, `{"league":"Standard","status":"online","widen_pct":10.0,"enable_all_stats":false}`, serde(t, opts()))
}

func TestTradeQueryKeepsIntegerNumbers(t *testing.T) {
	var q TradeQuery
	require.NoError(t, json.Unmarshal([]byte(`{"sort":{"price":"asc"},"query":{"b":1,"a":2.0,"c":1e2}}`), &q))
	assert.Equal(t, `{"query":{"a":2.0,"b":1,"c":100.0},"sort":{"price":"asc"}}`, serde(t, q))
}

func add(a, b float64) float64 { return a + b }
