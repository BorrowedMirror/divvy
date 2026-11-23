package trade

import (
	"encoding/json/v2"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/core"
)

// Unit tests.

func TestCandidateMapping(t *testing.T) {
	unique := func(class core.ItemClass) []ninjaCandidate {
		it := testItem(class, core.CategoryUnique, new("X"), "Y")
		return ninjaCandidates(&it)
	}
	stash := func(kinds ...string) []ninjaCandidate {
		out := make([]ninjaCandidate, len(kinds))
		for i, k := range kinds {
			out[i] = ninjaCandidate{endpointStash, k}
		}
		return out
	}
	assert.Equal(t, stash("UniqueAccessories"), unique(core.ClassRing.Class()))
	assert.Equal(t, stash("UniqueArmours"), unique(core.ClassHelmet.Class()))
	assert.Len(t, unique(core.ClassQuiver.Class()), 2)
	assert.Equal(t, stash("UniqueWeapons"), unique(core.WeaponClass("Bows")))
	assert.Equal(t, stash("UniqueFlasks"), unique(core.ClassLifeFlask.Class()))
	assert.Equal(t, stash("UniqueCharms"), unique(core.ClassCharm.Class()))
	assert.Equal(t, stash("UniqueJewels"), unique(core.ClassJewel.Class()))
	assert.Equal(t, stash("UniqueSanctumRelics"), unique(core.ClassRelic.Class()))
	assert.Len(t, unique(core.ClassTablet.Class()), 2)
	assert.Len(t, unique(core.OtherClass("?")), len(allStash))

	cur := func(class core.ItemClass) []ninjaCandidate {
		it := testItem(class, core.CategoryCurrency, nil, "Y")
		return ninjaCandidates(&it)
	}
	ex := func(kinds ...string) []ninjaCandidate {
		out := make([]ninjaCandidate, len(kinds))
		for i, k := range kinds {
			out[i] = ninjaCandidate{endpointExchange, k}
		}
		return out
	}
	assert.Equal(t, ex("Currency")[0], cur(core.ClassStackableCurrency.Class())[0])
	assert.Equal(t, ex("Ritual"), cur(core.ClassOmen.Class()))
	assert.Equal(t, ex("Essences"), cur(core.ClassEssence.Class()))
	assert.Equal(t, ex("SoulCores"), cur(core.ClassSoulCore.Class()))
	assert.Equal(t, ex("Runes"), cur(core.ClassRune.Class()))
	assert.Len(t, cur(core.ClassTalisman.Class()), 2)
	assert.Equal(t, ex("Delirium"), cur(core.ClassDistilledEmotion.Class()))
	assert.Len(t, cur(core.ClassSplinter.Class()), 4)
	assert.Equal(t, ex("Breach"), cur(core.ClassCatalyst.Class()))
	assert.Equal(t, ex("UncutGems"), cur(core.ClassUncutGem.Class()))
	assert.Equal(t, ex("LineageSupportGems"), cur(core.ClassSupportGem.Class()))
	assert.Len(t, cur(core.OtherClass("?")), len(allExchange))
}

func TestStashMatchFallsBackToNameOnly(t *testing.T) {
	var ov ninjaOverview
	require.NoError(t, json.Unmarshal([]byte(`{"core":{"primary":"divine","rates":{"exalted":0.0}},
		"lines":[{"id":1,"name":"Foo","baseType":"Other Base","primaryValue":2.0}]}`), &ov))
	it := testItem(core.ClassRing.Class(), core.CategoryUnique, new("Foo"), "Gold Ring")
	v, n, ok := ov.find(endpointStash, &it)
	assert.True(t, ok)
	assert.InDelta(t, 2.0, v, 0)
	assert.Equal(t, uint32(1), n)
	// Non-positive rates are dropped.
	assert.Empty(t, ov.rates().Rates)
	nameless := testItem(core.ClassRing.Class(), core.CategoryUnique, nil, "Gold Ring")
	_, _, ok = ov.find(endpointStash, &nameless)
	assert.False(t, ok)
}

func TestBadBaseURLIsConfigError(t *testing.T) {
	_, err := NewNinjaSource(SourceOptions{BaseURL: mustURL(t, "data:text/plain,x")})
	assert.Equal(t, KindConfig, asTradeErr(t, err).Kind)
	_, err = NewScoutSource(SourceOptions{})
	assert.Equal(t, KindConfig, asTradeErr(t, err).Kind)
}

func newNinja(t *testing.T, base *url.URL, ttl time.Duration) *NinjaSource {
	t.Helper()
	src, err := NewNinjaSource(SourceOptions{BaseURL: base, UserAgent: ninjaUA, CacheTTL: ttl})
	require.NoError(t, err)
	return src
}

// Contract tests against recorded poe.ninja fixtures.

const (
	ninjaUA       = "divvy-test/1.0 (+ninja)"
	ninjaLeague   = "Forbidden Rites"
	ninjaExchange = "/poe2/api/economy/exchange/current/overview"
	ninjaStash    = "/poe2/api/economy/stash/current/item/overview"
)

func ninjaRequest(item core.ParsedItem) *PriceRequest {
	base := item.BaseType
	return &PriceRequest{
		Filters: core.FilterSet{
			League:   ninjaLeague,
			Status:   core.StatusOnline,
			Name:     item.Name,
			BaseType: &base,
			WidenPct: 10,
			Route:    core.RouteExchange,
		},
		Item: item,
	}
}

func ninjaCurrency(base string) *PriceRequest {
	return ninjaRequest(testItem(core.ClassStackableCurrency.Class(), core.CategoryCurrency, nil, base))
}

func ninjaUnique(name, base string) *PriceRequest {
	return ninjaRequest(testItem(core.ClassBelt.Class(), core.CategoryUnique, new(name), base))
}

func ninjaSourceFor(t *testing.T, m *mockServer) *NinjaSource {
	t.Helper()
	return newNinja(t, m.baseURL(), DefaultSourceCacheTTL)
}

func mountNinjaCurrency(t *testing.T, m *mockServer, expect int) {
	t.Helper()
	m.mount(mockSpec{Method: "GET", Path: ninjaExchange, Expect: expect, Match: matchers(
		queryParam("league", ninjaLeague), queryParam("type", "Currency"), header("User-Agent", ninjaUA))},
		respond(200, fixtureBytes(t, "ninja/exchange-currency.json")))
}

func mountNinjaUniques(t *testing.T, m *mockServer, expect int) {
	t.Helper()
	m.mount(mockSpec{Method: "GET", Path: ninjaStash, Expect: expect, Match: matchers(
		queryParam("league", ninjaLeague), queryParam("type", "UniqueAccessories"), header("User-Agent", ninjaUA))},
		respond(200, fixtureBytes(t, "ninja/stash-unique-accessories.json")))
}

func approx(t *testing.T, p *core.Price, amount float64, currency string) {
	t.Helper()
	require.NotNil(t, p)
	assert.Equal(t, currency, p.Currency, "%+v", p)
	assert.InDelta(t, amount, p.Amount, 1e-3, "%+v != %v %s", p, amount, currency)
}

func TestNinjaPricesCurrencyWithNormalisation(t *testing.T) {
	m := newMock(t)
	mountNinjaCurrency(t, m, 1)
	src := ninjaSourceFor(t, m)
	assert.Equal(t, core.SourcePoeNinja, src.Kind())

	divine, err := src.Price(t.Context(), ninjaCurrency("Divine Orb"))
	require.NoError(t, err)
	assert.Equal(t, core.SourcePoeNinja, divine.Source)
	approx(t, divine.Median, 1.0, "divine")
	assert.Equal(t, divine.Min, divine.Max)
	assert.Empty(t, divine.Listings)
	assert.Nil(t, divine.TradeURL)

	// 0.001_380 divine * 724.8 exalted/divine ~= 1 exalted
	exalted, err := src.Price(t.Context(), ninjaCurrency("Exalted Orb"))
	require.NoError(t, err)
	approx(t, exalted.Median, 0.001_380*724.8, "exalted")

	alch, err := src.Price(t.Context(), ninjaCurrency("Orb of Alchemy"))
	require.NoError(t, err)
	approx(t, alch.Median, 0.005_949*724.8, "exalted")
}

func TestNinjaPricesUniquesInDivineOrExalted(t *testing.T) {
	m := newMock(t)
	mountNinjaUniques(t, m, 1)
	src := ninjaSourceFor(t, m)

	mb, err := src.Price(t.Context(), ninjaUnique("Mageblood", "Utility Belt"))
	require.NoError(t, err)
	approx(t, mb.Median, 130.0, "divine")
	assert.Equal(t, uint32(7570), mb.Total)

	andv := ninjaUnique("Andvarius", "Gold Ring")
	andv.Item = testItem(core.ClassRing.Class(), core.CategoryUnique, new("Andvarius"), "Gold Ring")
	p, err := src.Price(t.Context(), andv)
	require.NoError(t, err)
	// 0.07735 divine at 704.8 exalted/divine (rates in this fixture's core).
	approx(t, p.Median, 0.077_35*704.8, "exalted")
}

func TestNinjaUnknownItemIsUnsupported(t *testing.T) {
	m := newMock(t)
	mountNinjaUniques(t, m, 1)
	_, err := ninjaSourceFor(t, m).Price(t.Context(), ninjaUnique("Not A Real Belt", "Utility Belt"))
	te := asTradeErr(t, err)
	assert.Equal(t, KindUnsupported, te.Kind)
	assert.Equal(t, "poe.ninja", te.Source)
}

func TestNinjaCurrencyFallsThroughTypesUntilFound(t *testing.T) {
	m := newMock(t)
	// Currency overview lacks the item, Fragments has it.
	mountNinjaCurrency(t, m, 1)
	fragments := `{"core":{"primary":"divine","rates":{"exalted":700.0}},
		"lines":[{"id":"breach-splinter","primaryValue":2.5}],
		"items":[{"id":"breach-splinter","name":"Simulacrum"}]}`
	m.mount(mockSpec{Method: "GET", Path: ninjaExchange, Match: matchers(queryParam("type", "Fragments")), Expect: 1},
		respond(200, []byte(fragments)))
	p, err := ninjaSourceFor(t, m).Price(t.Context(), ninjaCurrency("Simulacrum"))
	require.NoError(t, err)
	approx(t, p.Median, 2.5, "divine")
}

func TestNinjaSupportsMatrix(t *testing.T) {
	src := newNinja(t, mustURL(t, "https://poe.ninja"), DefaultSourceCacheTTL)
	supports := func(class core.ItemClass, cat core.ItemCategory, name *string, base string) bool {
		it := testItem(class, cat, name, base)
		return src.Supports(&it)
	}
	assert.True(t, supports(core.ClassStackableCurrency.Class(), core.CategoryCurrency, nil, "Divine Orb"))
	assert.True(t, supports(core.ClassRing.Class(), core.CategoryUnique, new("Andvarius"), "Gold Ring"))
	assert.False(t, supports(core.ClassRing.Class(), core.CategoryUnique, nil, "Gold Ring"))
	assert.True(t, supports(core.ClassUncutGem.Class(), core.CategoryGem, nil, "Uncut Skill Gem"))
	assert.True(t, supports(core.ClassSupportGem.Class(), core.CategoryGem, nil, "Some Lineage Support"))
	assert.False(t, supports(core.ClassSkillGem.Class(), core.CategoryGem, nil, "Fireball"))
	assert.False(t, supports(core.ClassHelmet.Class(), core.CategoryEquipment, nil, "Expert Hunter Hood"))

	rare := ninjaRequest(testItem(core.ClassHelmet.Class(), core.CategoryEquipment, nil, "Expert Hunter Hood"))
	_, err := src.Price(t.Context(), rare)
	assert.Equal(t, KindUnsupported, asTradeErr(t, err).Kind)
}

func TestNinjaServerErrorMapsToHTTP(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "GET", Path: ninjaExchange, Expect: anyHits}, respond(503, []byte("upstream down")))
	_, err := ninjaSourceFor(t, m).Price(t.Context(), ninjaCurrency("Divine Orb"))
	te := asTradeErr(t, err)
	require.Equal(t, KindHTTP, te.Kind, "expected http error, got %v", err)
	assert.Equal(t, 503, te.Status)
	assert.Equal(t, "upstream down", te.Message)
}

func TestNinjaBadJSONMapsToDecode(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "GET", Path: ninjaStash, Expect: anyHits}, respond(200, []byte(`{"lines":`)))
	_, err := ninjaSourceFor(t, m).Price(t.Context(), ninjaUnique("Mageblood", "Utility Belt"))
	te := asTradeErr(t, err)
	assert.Equal(t, KindDecode, te.Kind)
	assert.Contains(t, te.Message, "UniqueAccessories")
}

func TestNinjaCachesOverviewsPerLeagueAndType(t *testing.T) {
	m := newMock(t)
	mountNinjaCurrency(t, m, 1)
	src := newNinja(t, m.baseURL(), 60*time.Second)
	_, err := src.Price(t.Context(), ninjaCurrency("Divine Orb"))
	require.NoError(t, err)
	_, err = src.Price(t.Context(), ninjaCurrency("Exalted Orb"))
	require.NoError(t, err)
	rates, err := src.Rates(t.Context(), ninjaLeague)
	require.NoError(t, err)
	assert.Equal(t, "divine", rates.Base)
	assert.InDelta(t, 1.0/724.8, rates.Rates["exalted"], 1e-12)
	assert.InDelta(t, 1.0/9.36, rates.Rates["chaos"], 1e-12)
	// Expect: 1 on the mock is verified at cleanup.
}

func TestNinjaTransportErrorSurfaces(t *testing.T) {
	// Nothing listens on this port.
	src := newNinja(t, mustURL(t, "http://127.0.0.1:9"), DefaultSourceCacheTTL)
	_, err := src.Rates(t.Context(), ninjaLeague)
	assert.Equal(t, KindTransport, asTradeErr(t, err).Kind)
}
