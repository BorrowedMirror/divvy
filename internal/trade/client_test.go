package trade

// Contract tests for the trade2 client against recorded fixtures (Rust tests/client.rs; see
// docs/research/trade2.md).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tradeUA = "rkl-test/0.0 (+https://example.invalid)"

func jsonFixture(t *testing.T, name string) any {
	t.Helper()
	v, err := decodeValue(fixtureBytes(t, name))
	require.NoError(t, err)
	return v
}

func tradeConfig(m *mockServer) TradeConfig {
	return TradeConfig{BaseURL: m.baseURL(), UserAgent: tradeUA, FetchCount: 20, CacheTTL: 60 * time.Second}
}

func newClient(t *testing.T, cfg TradeConfig) *TradeClient {
	t.Helper()
	c, err := NewTradeClient(cfg)
	require.NoError(t, err)
	return c
}

func testQuery() *core.TradeQuery {
	return &core.TradeQuery{
		Query: map[string]any{
			"status": map[string]any{"option": "online"},
			"name":   "Andvarius",
			"type":   "Gold Ring",
			"stats":  []any{map[string]any{"type": "and", "filters": []any{}}},
		},
		Sort: map[string]any{"price": "asc"},
	}
}

func otherQuery() *core.TradeQuery {
	q := testQuery()
	q.Sort = map[string]any{"price": "desc"}
	return q
}

func testFilters(route core.QueryRoute, base string) core.FilterSet {
	return core.FilterSet{
		League:   "Standard",
		Status:   core.StatusOnline,
		BaseType: &base,
		WidenPct: 10,
		Route:    route,
	}
}

func searchOK(t *testing.T) func() testkitResponse {
	return func() testkitResponse {
		return withHeaders(t, respond(200, fixtureBytes(t, "search.json")), "search.200")
	}
}

func fetchOK(t *testing.T) testkitResponse {
	return withHeaders(t, respond(200, fixtureBytes(t, "fetch.json")), "fetch.200")
}

func asTradeErr(t *testing.T, err error) *Error {
	t.Helper()
	var te *Error
	require.True(t, errors.As(err, &te), "expected *Error, got %v", err)
	return te
}

func TestLeaguesSendsUserAgentAndDecodes(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/data/leagues", Match: matchers(header("User-Agent", tradeUA)), Expect: 1},
		respond(200, fixtureBytes(t, "leagues.json")))
	c := newClient(t, tradeConfig(m))
	leagues, err := c.Leagues(t.Context())
	require.NoError(t, err)
	require.Len(t, leagues, 6)
	assert.Equal(t, "Standard", leagues[4].ID)
	assert.Equal(t, "poe2", *leagues[4].Realm)
	// second call is served from the cache (mock expects exactly one hit)
	again, err := c.Leagues(t.Context())
	require.NoError(t, err)
	assert.Len(t, again, 6)
}

func TestPoesessidIsSentAsCookie(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/data/stats", Match: matchers(header("Cookie", "POESESSID=abc123")), Expect: 1},
		respond(200, fixtureBytes(t, "stats.sample.json")))
	cfg := tradeConfig(m)
	cfg.POESESSID = "abc123"
	c := newClient(t, cfg)
	stats, err := c.Data(t.Context(), "stats")
	require.NoError(t, err)
	id, _ := asStr(index(asArray(index(stats, "result"))[0], "id"))
	assert.Equal(t, "pseudo", id)
}

func TestEmptyPoesessidSendsNoCookie(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/items", Expect: anyHits}, respond(200, fixtureBytes(t, "items.sample.json")))
	cfg := tradeConfig(m)
	cfg.POESESSID = ""
	c := newClient(t, cfg)
	_, err := c.Data(t.Context(), "items")
	require.NoError(t, err)
	reqs := m.received()
	require.NotEmpty(t, reqs)
	assert.Empty(t, reqs[0].Header.Values("Cookie"))
}

func TestInvalidConfigIsRejected(t *testing.T) {
	base := DefaultTradeConfig()
	// Rust's Url normalised this to `https://www.pathofexile.com/`; net/url keeps it as given.
	assert.Equal(t, "https://www.pathofexile.com", base.BaseURL.String())
	assert.True(t, strings.HasPrefix(base.UserAgent, "rkl/"))
	badURL := base
	badURL.BaseURL = mustURL(t, "mailto:a@b.c")
	_, err := NewTradeClient(badURL)
	assert.Equal(t, KindConfig, asTradeErr(t, err).Kind)
	badCookie := base
	badCookie.POESESSID = "a\nb"
	_, err = NewTradeClient(badCookie)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "POESESSID")
}

func TestSearchPostsBodyToRealmPathAndCaches(t *testing.T) {
	m := newMock(t)
	q := testQuery()
	body, err := core.MarshalSerde(q)
	require.NoError(t, err)
	m.mount(mockSpec{
		Method: "POST", Path: "/api/trade2/search/poe2/Standard",
		Match:  matchers(header("User-Agent", tradeUA), header("Content-Type", "application/json"), bodyJSON(t, body)),
		Expect: 1,
	}, searchOK(t)())
	c := newClient(t, tradeConfig(m))
	r, err := c.Search(t.Context(), "Standard", q)
	require.NoError(t, err)
	assert.Equal(t, uint32(11), r.Total)
	assert.Len(t, r.Result, 11)
	assert.Equal(t, uint32(6), *r.Complexity)
	again, err := c.Search(t.Context(), "Standard", q)
	require.NoError(t, err)
	assert.Equal(t, r, again)
	name, _ := c.Limiter().PolicyName("search")
	assert.Equal(t, "trade-search-request-limit", name)
}

func TestZeroTTLDisablesCache(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: 2}, searchOK(t)())
	cfg := tradeConfig(m)
	cfg.CacheTTL = 0
	c := newClient(t, cfg)
	_, err := c.Search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	_, err = c.Search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
}

func TestLeagueWithSpacesIsPercentEncoded(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Path: "/api/trade2/search/poe2/Forbidden%20Rites", Expect: 1}, searchOK(t)())
	c := newClient(t, tradeConfig(m))
	r, err := c.Search(t.Context(), "Forbidden Rites", testQuery())
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%s/trade2/search/poe2/Forbidden%%20Rites/%s", m.URI(), r.ID), c.TradeURL("Forbidden Rites", r.ID))
}

func TestTradeURLsMatchSiteFormat(t *testing.T) {
	c := newClient(t, DefaultTradeConfig())
	assert.Equal(t, "https://www.pathofexile.com/trade2/search/poe2/Standard/abc", c.TradeURL("Standard", "abc"))
	assert.Equal(t, "https://www.pathofexile.com/trade2/exchange/poe2/HC%20Runes%20of%20Aldur/xyz",
		c.ExchangeURL("HC Runes of Aldur", "xyz"))
}

func TestFetchChunksIDsByTenAndSkipsNulls(t *testing.T) {
	m := newMock(t)
	ids := make([]string, 12)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%d", i)
	}
	first := strings.Join(ids[:FetchChunk], ",")
	second := strings.Join(ids[FetchChunk:], ",")
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + first,
		Match: matchers(queryParam("query", "Q1"), queryParam("realm", "poe2")), Expect: 1}, fetchOK(t))
	tail := jsonFixture(t, "fetch.json").(map[string]any)
	one := asArray(tail["result"])[0]
	tail["result"] = []any{one, nil}
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + second, Expect: 1}, respondJSON(t, 200, tail))
	c := newClient(t, tradeConfig(m))
	entries, err := c.Fetch(t.Context(), ids, "Q1")
	require.NoError(t, err)
	assert.Len(t, entries, 7)
	// cached per chunk
	entries, err = c.Fetch(t.Context(), ids, "Q1")
	require.NoError(t, err)
	assert.Len(t, entries, 7)
}

func TestFetchChunksRunConcurrentlyInIDOrder(t *testing.T) {
	m := newMock(t)
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%d", i)
	}
	delay := 500 * time.Millisecond
	second := jsonFixture(t, "fetch.json").(map[string]any)
	second["result"] = []any{asArray(second["result"])[1]}
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + strings.Join(ids[:FetchChunk], ","), Delay: delay, Expect: 1},
		fetchOK(t))
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + strings.Join(ids[FetchChunk:], ","), Delay: delay, Expect: 1},
		respondJSON(t, 200, second))
	c := newClient(t, tradeConfig(m))
	start := time.Now()
	entries, err := c.Fetch(t.Context(), ids, "Q1")
	require.NoError(t, err)
	// Sequential chunks would take at least 2 * delay.
	assert.Less(t, time.Since(start), 900*time.Millisecond)
	var first ResultList[FetchEntry]
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "fetch.json"), &first))
	assert.Len(t, entries, len(first.Result)+1)
	wantID, _ := asStr(index(asArray(second["result"])[0], "id"))
	assert.Equal(t, wantID, entries[len(entries)-1].ID)
}

// readSnapshot returns the body of an insta .snap file (after the YAML front matter).
func readSnapshot(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(tradeFixturesDir()), "snapshots", name))
	require.NoError(t, err)
	parts := strings.SplitN(string(b), "---\n", 3)
	require.Len(t, parts, 3, "snapshot front matter")
	return strings.TrimRight(parts[2], "\n")
}

func TestSearchListingsMapsFetchEntries(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, searchOK(t)())
	m.mount(mockSpec{Method: "GET", Expect: 1}, fetchOK(t))
	cfg := tradeConfig(m)
	cfg.FetchCount = 6
	c := newClient(t, cfg)
	page, err := c.SearchListings(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	assert.Equal(t, uint32(11), page.Total)
	require.Len(t, page.Listings, 6)
	l := page.Listings[0]
	assert.Equal(t, "Seller1#0001", *l.Seller)
	assert.InDelta(t, 1.0, l.Price.Amount, 1e-15)
	assert.Equal(t, "divine", l.Price.Currency)
	assert.True(t, l.Online)
	assert.Equal(t, uint32(40), *l.ItemLevel)
	assert.Equal(t, "2025-01-13T21:21:29Z", *l.Indexed)
	assert.True(t, strings.HasSuffix(page.TradeURL, "/trade2/search/poe2/Standard/"+page.SearchID))

	// insta::assert_json_snapshot!("search_listings", page.listings): byte-identical to
	// serde_json's pretty printer.
	got, err := core.MarshalSerde(page.Listings)
	require.NoError(t, err)
	var pretty bytes.Buffer
	require.NoError(t, json.Indent(&pretty, got, "", "  "))
	assert.Equal(t, readSnapshot(t, "client__search_listings.snap"), pretty.String())
}

func TestEmptySearchSkipsFetch(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, respondJSON(t, 200, map[string]any{"id": "E", "result": []any{}, "total": 0}))
	m.mount(mockSpec{Method: "GET", Expect: 0}, fetchOK(t))
	page, err := newClient(t, tradeConfig(m)).SearchListings(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	assert.Equal(t, uint32(0), page.Total)
	assert.Empty(t, page.Listings)
}

func TestListingMappingEdgeCases(t *testing.T) {
	var list ResultList[FetchEntry]
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "fetch.json"), &list))
	e := list.Result[0]
	e.Listing.Account.Online = nil
	e.Item = map[string]any{"stackSize": json.Number("7")}
	l, ok := ListingFromFetch(&e)
	require.True(t, ok)
	assert.False(t, l.Online)
	assert.Equal(t, uint32(7), *l.StackSize)
	assert.Nil(t, l.ItemLevel)
	e.Listing.Price = nil
	_, ok = ListingFromFetch(&e)
	assert.False(t, ok)

	var x ExchangeResponse
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "exchange.json"), &x))
	entry := x.clone().Result[0]
	entry.Listing.Offers[0].Item.Amount = 0
	_, ok = ListingFromExchange(&entry)
	assert.False(t, ok)
	entry.Listing.Offers = nil
	_, ok = ListingFromExchange(&entry)
	assert.False(t, ok)
}

func TestTooManyRequestsMapsRetryAfterAndBlocksFurtherCalls(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: 1}, withHeaders(t, respond(429, []byte("{}")), "search.429"))
	c := newClient(t, tradeConfig(m))
	_, err := c.Search(t.Context(), "Standard", testQuery())
	te := asTradeErr(t, err)
	require.Equal(t, KindRateLimited, te.Kind, "expected rate limit, got %v", err)
	assert.True(t, te.RetryAfter > 59*time.Second && te.RetryAfter <= 60*time.Second, "%v", te.RetryAfter)
	info := te.Info()
	assert.Equal(t, "rate_limited", info.Kind)
	assert.Greater(t, *info.RetryAfterMs, uint64(59_000))
	// the limiter now refuses locally without touching the server (Expect: 1 above)
	_, err = c.Search(t.Context(), "Standard", testQuery())
	assert.Equal(t, KindRateLimited, asTradeErr(t, err).Kind)
}

func TestRateLimitHeadersThrottleSubsequentCalls(t *testing.T) {
	m := newMock(t)
	resp := searchOK(t)()
	resp.Header.Set("x-rate-limit-ip", "1:60:60")
	resp.Header.Set("x-rate-limit-ip-state", "1:60:0")
	m.mount(mockSpec{Method: "POST", Expect: 1}, resp)
	c := newClient(t, tradeConfig(m))
	_, err := c.Search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	// A different query is not cached; the 1-per-60s rule makes it wait ~60s, which is
	// beyond MaxQueueWait, so it fails fast without a request.
	_, err = c.Search(t.Context(), "Standard", otherQuery())
	te := asTradeErr(t, err)
	require.Equal(t, KindRateLimited, te.Kind)
	assert.Greater(t, te.RetryAfter, 50*time.Second)
	// Refusals record nothing: retrying does not push the next free slot further out.
	for range 3 {
		_, err := c.Search(t.Context(), "Standard", otherQuery())
		again := asTradeErr(t, err)
		require.Equal(t, KindRateLimited, again.Kind)
		assert.LessOrEqual(t, again.RetryAfter, te.RetryAfter)
	}
}

func TestCancelledWaitsReleaseTheirSlot(t *testing.T) {
	m := newMock(t)
	resp := searchOK(t)()
	resp.Header.Set("x-rate-limit-ip", "1:2:0")
	resp.Header.Set("x-rate-limit-ip-state", "1:2:0")
	m.mount(mockSpec{Method: "POST", Expect: 1}, resp)
	c := newClient(t, tradeConfig(m))
	_, err := c.Search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	// The caller gives up while the request is queued behind the limiter.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = c.Search(ctx, "Standard", otherQuery())
	require.ErrorIs(t, err, context.DeadlineExceeded, "request should still have been waiting")
	// Its slot was handed back: the next request waits for one padded window (2.25s), not
	// two (~4.5s).
	next, ok := c.Limiter().TryReserve("search", 0)
	require.False(t, ok)
	assert.LessOrEqual(t, next, 2_250*time.Millisecond)
}

func TestDataEndpointsAreThrottledBeforeAnyHeaders(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/stats", Expect: anyHits}, respond(200, fixtureBytes(t, "stats.sample.json")))
	cfg := tradeConfig(m)
	cfg.CacheTTL = 0
	c := newClient(t, cfg)
	for range 5 {
		_, err := c.Data(t.Context(), "stats")
		require.NoError(t, err)
	}
	// Seeded 5:10 rule: the sixth call inside 10s would queue ~10.5s > MaxQueueWait.
	_, err := c.Data(t.Context(), "stats")
	assert.Equal(t, KindRateLimited, asTradeErr(t, err).Kind)
}

func TestShortWaitsAreSleptThrough(t *testing.T) {
	m := newMock(t)
	resp := searchOK(t)()
	resp.Header.Set("x-rate-limit-ip", "1:1:0")
	resp.Header.Set("x-rate-limit-ip-state", "1:1:0")
	m.mount(mockSpec{Method: "POST", Expect: 2}, resp)
	c := newClient(t, tradeConfig(m))
	_, err := c.Search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	start := time.Now()
	_, err = c.Search(t.Context(), "Standard", otherQuery())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond)
}

func TestServerErrorsMapToHTTP(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/leagues", Expect: anyHits},
		respondJSON(t, 503, map[string]any{"error": map[string]any{"code": 0, "message": "Maintenance"}}))
	m.mount(mockSpec{Path: "/api/trade2/data/stats", Expect: anyHits}, respond(500, []byte("upstream exploded")))
	c := newClient(t, tradeConfig(m))
	_, err := c.Leagues(t.Context())
	te := asTradeErr(t, err)
	assert.Equal(t, KindHTTP, te.Kind)
	assert.Equal(t, uint16(503), te.Status)
	assert.Equal(t, "Maintenance", te.Message)
	assert.Equal(t, "http", te.Info().Kind)
	_, err = c.Data(t.Context(), "stats")
	te = asTradeErr(t, err)
	assert.Equal(t, KindHTTP, te.Kind)
	assert.Equal(t, uint16(500), te.Status)
	assert.Equal(t, "upstream exploded", te.Message)
}

func TestInvalidJSONMapsToDecode(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, respond(200, []byte("<html>")))
	m.mount(mockSpec{Path: "/api/trade2/data/leagues", Expect: anyHits}, respondJSON(t, 200, map[string]any{"result": 5}))
	c := newClient(t, tradeConfig(m))
	_, err := c.Search(t.Context(), "Standard", testQuery())
	te := asTradeErr(t, err)
	assert.Equal(t, KindDecode, te.Kind)
	assert.Equal(t, "decode", te.Info().Kind)
	_, err = c.Leagues(t.Context())
	assert.Equal(t, KindDecode, asTradeErr(t, err).Kind)
}

func TestTransportErrorsAreReported(t *testing.T) {
	// A server that hangs up on every connection.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	cfg := DefaultTradeConfig()
	cfg.BaseURL = mustURL(t, "http://"+l.Addr().String())
	c := newClient(t, cfg)
	_, err = c.Leagues(t.Context())
	te := asTradeErr(t, err)
	assert.Equal(t, KindTransport, te.Kind, "%v", err)
	assert.Equal(t, "transport", te.Info().Kind)
}

func TestExchangeBodyShape(t *testing.T) {
	b, err := core.MarshalSerde(ExchangeBody("divine", core.StatusSecurable))
	require.NoError(t, err)
	assert.JSONEq(t, `{"query":{"status":{"option":"online"},"have":["exalted"],"want":["divine"]},"sort":{"have":"asc"},"engine":"new"}`, string(b))
	// serde_json::Value (no preserve_order) writes keys sorted.
	assert.Equal(t, `{"engine":"new","query":{"have":["exalted"],"status":{"option":"online"},"want":["divine"]},"sort":{"have":"asc"}}`, string(b))
	q := func(want string, s core.OnlineStatus) map[string]any {
		return ExchangeBody(want, s)["query"].(map[string]any)
	}
	assert.Equal(t, []any{"exalted", "divine"}, q("chaos", core.StatusAny)["have"])
	assert.Equal(t, "any", q("chaos", core.StatusAny)["status"].(map[string]any)["option"])
	assert.Equal(t, []any{"divine"}, q("exalted", core.StatusOnline)["have"])
}

func TestExchangeRouteResolvesStaticIDAndMapsOffers(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/static", Expect: 1}, respond(200, fixtureBytes(t, "static.sample.json")))
	body, err := core.MarshalSerde(ExchangeBody("transmute", core.StatusOnline))
	require.NoError(t, err)
	m.mount(mockSpec{Method: "POST", Path: "/api/trade2/exchange/poe2/Standard", Match: matchers(bodyJSON(t, body)), Expect: 1},
		withHeaders(t, respond(200, fixtureBytes(t, "exchange.json")), "exchange.200"))
	c := newClient(t, tradeConfig(m))
	f := testFilters(core.RouteExchange, "Orb of Transmutation")
	page, err := c.ListingsFor(t.Context(), &f)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), page.Total)
	require.Len(t, page.Listings, 3)
	assert.Equal(t, "exalted", page.Listings[0].Price.Currency)
	assert.Equal(t, uint32(2), *page.Listings[0].StackSize)
	assert.False(t, page.Listings[0].Online)
	assert.True(t, page.Listings[1].Online)
	assert.Contains(t, page.TradeURL, "/trade2/exchange/poe2/Standard/")
	// cached: second lookup hits neither endpoint again
	again, err := c.ListingsFor(t.Context(), &f)
	require.NoError(t, err)
	assert.Equal(t, page, again)
	name, _ := c.Limiter().PolicyName("exchange")
	assert.Equal(t, "trade-exchange-request-limit", name)
}

func TestExchangeRouteHandlesEmptyAndUnknown(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/static", Expect: anyHits}, respond(200, fixtureBytes(t, "static.sample.json")))
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, respond(200, fixtureBytes(t, "exchange.empty.json")))
	c := newClient(t, tradeConfig(m))
	f := testFilters(core.RouteExchange, "Scroll of Wisdom")
	page, err := c.ListingsFor(t.Context(), &f)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), page.Total)
	assert.Empty(t, page.Listings)
	f = testFilters(core.RouteExchange, "Mirror of Nothing")
	_, err = c.ListingsFor(t.Context(), &f)
	assert.Equal(t, KindUnsupported, asTradeErr(t, err).Kind)
	id, ok, err := c.StaticID(t.Context(), "Orb of Transmutation")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "transmute", id)
}

func TestTradeClientIsAPriceSourceForEverything(t *testing.T) {
	c := newClient(t, DefaultTradeConfig())
	var src PriceSource = c
	assert.Equal(t, core.SourceTrade, src.Kind())
	assert.Equal(t, 20, c.Config().FetchCount)
}

// Full Price() path: FilterSet.ToTradeQuery + search + fetch + core.Summarise.
func TestPriceFullSearchPath(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, searchOK(t)())
	m.mount(mockSpec{Method: "GET", Expect: anyHits}, fetchOK(t))
	c := newClient(t, tradeConfig(m))
	f := testFilters(core.RouteSearch, "Gold Ring")
	f.Name = new("Andvarius")
	item, err := core.ParseItem("Item Class: Rings\nRarity: Unique\nAndvarius\nGold Ring\n--------\nItem Level: 40\n")
	require.NoError(t, err)
	summary, err := c.Price(t.Context(), &PriceRequest{Item: *item, Filters: f})
	require.NoError(t, err)
	assert.Equal(t, core.SourceTrade, summary.Source)
	assert.Equal(t, uint32(11), summary.Total)
	assert.NotNil(t, summary.Min)
	require.NotNil(t, summary.TradeURL)
	assert.Contains(t, *summary.TradeURL, "/trade2/search/poe2/Standard/")
}

func TestCurrencyIconsComeFromStaticData(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/static", Expect: 1}, respond(200, fixtureBytes(t, "static.sample.json")))
	c := newClient(t, tradeConfig(m))
	icons, err := c.CurrencyIcons(t.Context())
	require.NoError(t, err)
	var wisdom *CurrencyIcon
	count := 0
	for i := range icons {
		if icons[i].ID == "wisdom" {
			count++
			if wisdom == nil {
				wisdom = &icons[i]
			}
		}
	}
	require.NotNil(t, wisdom, "wisdom icon")
	assert.Equal(t, "Scroll of Wisdom", wisdom.Name)
	assert.True(t, strings.HasPrefix(wisdom.Icon, "https://web.poecdn.com/gen/image/"), wisdom.Icon)
	assert.Equal(t, 1, count)
	// Cached: the second call does not hit the server again (Expect: 1).
	again, err := c.CurrencyIcons(t.Context())
	require.NoError(t, err)
	assert.Equal(t, icons, again)
}

func TestCurrencyIconsSkipMissingImagesAndKeepAbsoluteURLs(t *testing.T) {
	v, err := decodeValue([]byte(`{"result": [{"id": "Currency", "entries": [
		{"id": "a", "text": "A", "image": "/gen/a.png"},
		{"id": "b", "text": "B"},
		{"id": "c", "image": "https://cdn.example/c.png"},
		{"id": "a", "text": "dup", "image": "/gen/x.png"}
	]}]}`))
	require.NoError(t, err)
	icons := CurrencyIconsFrom(v)
	require.Len(t, icons, 2)
	assert.Equal(t, "https://web.poecdn.com/gen/a.png", icons[0].Icon)
	assert.Equal(t, [2]string{"c", "https://cdn.example/c.png"}, [2]string{icons[1].Name, icons[1].Icon})
}

// Go-only: url segment / query encoding follows the url crate (`,` literal in paths, form
// encoding in queries) and Data hands out independent copies of the cached value.
func TestURLEncodingAndDataCopies(t *testing.T) {
	u := appendSegments(mustURL(t, "http://h/base/"), "api", "a,b c?/%")
	appendQuery(u, [2]string{"query", "x y*~"})
	assert.Equal(t, "http://h/base/api/a,b%20c%3F%2F%25?query=x+y*%7E", u.String())

	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/stats", Expect: 1}, respond(200, fixtureBytes(t, "stats.sample.json")))
	c := newClient(t, tradeConfig(m))
	v, err := c.Data(t.Context(), "stats")
	require.NoError(t, err)
	v.(map[string]any)["result"] = nil
	v2, err := c.Data(t.Context(), "stats")
	require.NoError(t, err)
	assert.NotNil(t, index(v2, "result"))
}
