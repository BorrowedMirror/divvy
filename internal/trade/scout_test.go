package trade

// poe2scout contract tests against recorded fixtures.

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

const (
	scoutUA        = "rkl-test/0 (+scout)"
	scoutLeagueStr = "Forbidden Rites"
	scoutItemsPath = "/poe2/Leagues/Forbidden%20Rites/Items"
)

func scoutCurrency(base string) core.ParsedItem {
	return testItem(core.ClassStackableCurrency.Class(), core.CategoryCurrency, nil, base)
}

func scoutUnique(name, base string) core.ParsedItem {
	return testItem(core.ClassBelt.Class(), core.CategoryUnique, new(name), base)
}

func scoutRequest(item core.ParsedItem, league string) *PriceRequest {
	return &PriceRequest{
		Item:    item,
		Filters: core.FilterSet{League: league, Status: core.StatusOnline, WidenPct: 10, Route: core.RouteExchange},
	}
}

func jsonResp(body []byte) response {
	r := respond(200, body)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func scoutServer(t *testing.T) *mockServer {
	t.Helper()
	m := newMock(t)
	m.mount(mockSpec{Method: "GET", Path: "/poe2/Leagues", Match: matchers(header("User-Agent", scoutUA)), Expect: anyHits},
		jsonResp(fixtureBytes(t, "scout/leagues.json")))
	m.mount(mockSpec{Method: "GET", Path: scoutItemsPath, Match: matchers(header("User-Agent", scoutUA)), Expect: anyHits},
		jsonResp(fixtureBytes(t, "scout/items.json")))
	return m
}

func newScout(t *testing.T, base *url.URL, ttl time.Duration) *ScoutSource {
	t.Helper()
	src, err := NewScoutSource(SourceOptions{BaseURL: base, UserAgent: scoutUA, CacheTTL: ttl})
	require.NoError(t, err)
	return src
}

func scoutSourceFor(t *testing.T, m *mockServer) *ScoutSource {
	t.Helper()
	return newScout(t, m.baseURL(), DefaultSourceCacheTTL)
}

func priceOf(t *testing.T, src *ScoutSource, it core.ParsedItem) core.Price {
	t.Helper()
	summary, err := src.Price(t.Context(), scoutRequest(it, scoutLeagueStr))
	require.NoError(t, err, "priced")
	assert.Equal(t, core.SourcePoe2Scout, summary.Source)
	assert.Equal(t, summary.Min, summary.Max)
	require.NotNil(t, summary.Median, "median")
	return *summary.Median
}

func TestScoutPricesCurrencyInDivineAndExalted(t *testing.T) {
	src := scoutSourceFor(t, scoutServer(t))
	assert.Equal(t, core.SourcePoe2Scout, src.Kind())
	div := priceOf(t, src, scoutCurrency("Divine Orb"))
	assert.Equal(t, "divine", div.Currency)
	assert.InDelta(t, 1.0, div.Amount, 1e-9)
	alch := priceOf(t, src, scoutCurrency("Orb of Alchemy"))
	assert.Equal(t, "exalted", alch.Currency)
	assert.InDelta(t, 3.9217, alch.Amount, 1e-3)
}

func TestScoutPricesUniques(t *testing.T) {
	src := scoutSourceFor(t, scoutServer(t))
	mb := priceOf(t, src, scoutUnique("Mageblood", "Utility Belt"))
	assert.Equal(t, "divine", mb.Currency)
	assert.InDelta(t, 70000.0/966.563_873_384_240_5, mb.Amount, 1e-6)
	andv := priceOf(t, src, testItem(core.ClassRing.Class(), core.CategoryUnique, new("Andvarius"), "Gold Ring"))
	assert.Equal(t, core.Price{Amount: 15, Currency: "exalted"}, andv)
	// Base type mismatch falls back to the name alone.
	hh := priceOf(t, src, scoutUnique("Headhunter", "Some Other Belt"))
	assert.Equal(t, "divine", hh.Currency)
}

func TestScoutPricesUncutGemByLevel(t *testing.T) {
	src := scoutSourceFor(t, scoutServer(t))
	gem := testItem(core.ClassUncutGem.Class(), core.CategoryGem, nil, "Uncut Support Gem")
	gem.GemLevel = new(uint32(2))
	assert.Equal(t, core.Price{Amount: 20, Currency: "exalted"}, priceOf(t, src, gem))
}

func TestScoutUnknownItemAndLeagueAreUnsupported(t *testing.T) {
	src := scoutSourceFor(t, scoutServer(t))
	check := func(err error) {
		t.Helper()
		te := asTradeErr(t, err)
		assert.Equal(t, KindUnsupported, te.Kind)
		assert.Equal(t, "poe2scout", te.Source)
	}
	_, err := src.Price(t.Context(), scoutRequest(scoutCurrency("Mirror of Kalandra"), scoutLeagueStr))
	check(err)
	_, err = src.Price(t.Context(), scoutRequest(scoutCurrency("Divine Orb"), "Nope"))
	check(err)
	rare := testItem(core.ClassHelmet.Class(), core.CategoryEquipment, nil, "Hood")
	_, err = src.Price(t.Context(), scoutRequest(rare, scoutLeagueStr))
	check(err)
}

func TestScoutNullPriceIsUnsupported(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/poe2/Leagues", Expect: anyHits}, jsonResp(fixtureBytes(t, "scout/leagues.json")))
	m.mount(mockSpec{Path: scoutItemsPath, Expect: anyHits},
		jsonResp([]byte(`[{"ItemId":1,"CategoryApiId":"currency","Text":"Divine Orb","CurrentPrice":null}]`)))
	_, err := scoutSourceFor(t, m).Price(t.Context(), scoutRequest(scoutCurrency("Divine Orb"), scoutLeagueStr))
	assert.Equal(t, KindUnsupported, asTradeErr(t, err).Kind)
}

func TestScoutSupportsMatrix(t *testing.T) {
	src := newScout(t, mustURL(t, "http://localhost"), DefaultSourceCacheTTL)
	sup := func(it core.ParsedItem) bool { return src.Supports(&it) }
	assert.True(t, sup(scoutCurrency("Divine Orb")))
	assert.True(t, sup(testItem(core.ClassUncutGem.Class(), core.CategoryGem, nil, "Uncut Skill Gem")))
	assert.True(t, sup(scoutUnique("Mageblood", "Utility Belt")))
	assert.False(t, sup(testItem(core.ClassBelt.Class(), core.CategoryUnique, nil, "Utility Belt")))
	assert.False(t, sup(testItem(core.ClassHelmet.Class(), core.CategoryEquipment, nil, "Hood")))
	assert.False(t, sup(testItem(core.ClassWaystone.Class(), core.CategoryWaystone, nil, "Waystone (Tier 15)")))
}

func TestScoutServerErrorMapsToHTTP(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/poe2/Leagues", Expect: anyHits}, respond(503, []byte("down")))
	_, err := scoutSourceFor(t, m).Rates(t.Context(), scoutLeagueStr)
	te := asTradeErr(t, err)
	require.Equal(t, KindHTTP, te.Kind, "unexpected %v", err)
	assert.Equal(t, 503, te.Status)
	assert.Equal(t, "down", te.Message)
}

func TestScoutBadJSONMapsToDecode(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/poe2/Leagues", Expect: anyHits}, jsonResp(fixtureBytes(t, "scout/leagues.json")))
	m.mount(mockSpec{Path: scoutItemsPath, Expect: anyHits}, jsonResp([]byte("{not json")))
	_, err := scoutSourceFor(t, m).Price(t.Context(), scoutRequest(scoutCurrency("Divine Orb"), scoutLeagueStr))
	te := asTradeErr(t, err)
	assert.Equal(t, KindDecode, te.Kind)
	assert.True(t, strings.HasPrefix(te.Message, "poe2scout"), te.Message)
}

func TestScoutCachesLeaguesAndItems(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "GET", Path: "/poe2/Leagues", Expect: 1}, jsonResp(fixtureBytes(t, "scout/leagues.json")))
	m.mount(mockSpec{Method: "GET", Path: scoutItemsPath, Expect: 1}, jsonResp(fixtureBytes(t, "scout/items.json")))
	src := newScout(t, m.baseURL(), 60*time.Second)
	priceOf(t, src, scoutCurrency("Divine Orb"))
	priceOf(t, src, scoutCurrency("Chaos Orb"))
	priceOf(t, src, scoutUnique("Mageblood", "Utility Belt"))
}

func TestScoutRatesFromLeagueEntry(t *testing.T) {
	src := scoutSourceFor(t, scoutServer(t))
	r, err := src.Rates(t.Context(), "Standard")
	require.NoError(t, err)
	assert.Equal(t, "exalted", r.Base)
	assert.InDelta(t, 195.887_294_117_647_03, r.Rates["divine"], 1e-9)
	assert.InDelta(t, 195.887_294_117_647_03/1.843_645_121_107_266_2, r.Rates["chaos"], 1e-9)
}

func TestScoutRatesTolerateMissingFields(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/poe2/Leagues", Expect: anyHits},
		jsonResp([]byte(`[{"Value":"X","DivinePrice":0},{"Value":"Y","DivinePrice":100,"ChaosDivinePrice":0}]`)))
	src := scoutSourceFor(t, m)
	x, err := src.Rates(t.Context(), "X")
	require.NoError(t, err)
	assert.Equal(t, "exalted", x.Base)
	assert.Empty(t, x.Rates)
	y, err := src.Rates(t.Context(), "Y")
	require.NoError(t, err)
	assert.Len(t, y.Rates, 1)
}

func TestScoutBaseURLWithPathPrefix(t *testing.T) {
	m := scoutServer(t)
	// Trailing slash on the base URL must not produce `//poe2`.
	src := newScout(t, mustURL(t, m.URL()+"/"), DefaultSourceCacheTTL)
	assert.Equal(t, "/", src.baseURL.Path)
	priceOf(t, src, scoutCurrency("Exalted Orb"))
}
