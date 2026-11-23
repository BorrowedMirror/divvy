package trade

// Contract tests for the trade2 client against recorded fixtures (see
// docs/research/trade2.md).

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/core"
)

const tradeUA = "divvy-test/0.0 (+https://example.invalid)"

func tradeConfig(m *mockServer) Config {
	return Config{BaseURL: m.baseURL(), UserAgent: tradeUA, FetchCount: 20, CacheTTL: 60 * time.Second}
}

func newClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := NewClient(cfg)
	require.NoError(t, err)
	return c
}

func testQuery() *core.TradeQuery {
	return &core.TradeQuery{
		Query: core.SearchQuery{
			Status: core.Option{Option: "online"},
			Name:   new("Andvarius"),
			Type:   new("Gold Ring"),
			Stats:  []core.StatGroup{{Type: "and", Filters: []core.StatCondition{}}},
		},
		Sort: core.SearchSort{Price: "asc"},
	}
}

func otherQuery() *core.TradeQuery {
	q := testQuery()
	q.Sort.Price = "desc"
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

func searchOK(t *testing.T) response {
	t.Helper()
	return withHeaders(t, respond(200, fixtureBytes(t, "search.json")), "search.200")
}

func fetchOK(t *testing.T) response {
	t.Helper()
	return withHeaders(t, respond(200, fixtureBytes(t, "fetch.json")), "fetch.200")
}

func asTradeErr(t *testing.T, err error) *Error {
	t.Helper()
	te, ok := errors.AsType[*Error](err)
	require.True(t, ok, "expected *Error, got %v", err)
	return te
}

// fetchResults is the `result` list of the recorded fetch fixture.
func fetchResults(t *testing.T) []any {
	t.Helper()
	return decodeJSON(t, fixtureBytes(t, "fetch.json")).(map[string]any)["result"].([]any)
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
	// The second call is served from the cache (the mock expects exactly one hit), decoded
	// afresh: changing one result does not change the next.
	leagues[4].ID = "changed"
	again, err := c.Leagues(t.Context())
	require.NoError(t, err)
	require.Len(t, again, 6)
	assert.Equal(t, "Standard", again[4].ID)
}

func TestPoesessidIsSentAsCookie(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/data/stats", Match: matchers(header("Cookie", "POESESSID=abc123")), Expect: 1},
		respond(200, fixtureBytes(t, "stats.sample.json")))
	cfg := tradeConfig(m)
	cfg.POESESSID = "abc123"
	c := newClient(t, cfg)
	_, err := c.dataBody(t.Context(), "stats")
	require.NoError(t, err)
}

func TestEmptyPoesessidSendsNoCookie(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/items", Expect: anyHits}, respond(200, fixtureBytes(t, "items.sample.json")))
	cfg := tradeConfig(m)
	cfg.POESESSID = ""
	c := newClient(t, cfg)
	_, err := c.dataBody(t.Context(), "items")
	require.NoError(t, err)
	reqs := m.received()
	require.NotEmpty(t, reqs)
	assert.Empty(t, reqs[0].Header.Values("Cookie"))
}

func TestInvalidConfigIsRejected(t *testing.T) {
	base := DefaultConfig("dev")
	assert.Equal(t, "https://www.pathofexile.com", base.BaseURL.String())
	assert.True(t, strings.HasPrefix(base.UserAgent, "divvy/dev "))
	for _, bad := range []string{"mailto:a@b.c", "/relative", "ftp://host"} {
		cfg := base
		cfg.BaseURL = mustURL(t, bad)
		_, err := NewClient(cfg)
		assert.Equal(t, KindConfig, asTradeErr(t, err).Kind, bad)
	}
	badCookie := base
	badCookie.POESESSID = "a\nb"
	_, err := NewClient(badCookie)
	require.ErrorContains(t, err, "POESESSID")
}

func TestSearchPostsBodyToRealmPathAndCaches(t *testing.T) {
	m := newMock(t)
	q := testQuery()
	body, err := json.Marshal(q)
	require.NoError(t, err)
	m.mount(mockSpec{
		Method: "POST", Path: "/api/trade2/search/poe2/Standard",
		Match:  matchers(header("User-Agent", tradeUA), header("Content-Type", "application/json"), bodyJSON(t, body)),
		Expect: 1,
	}, searchOK(t))
	c := newClient(t, tradeConfig(m))
	r, err := c.search(t.Context(), "Standard", q)
	require.NoError(t, err)
	assert.Equal(t, uint32(11), r.Total)
	assert.Len(t, r.Result, 11)
	assert.Equal(t, uint32(6), *r.Complexity)
	again, err := c.search(t.Context(), "Standard", q)
	require.NoError(t, err)
	assert.Equal(t, r, again)
	name, _ := c.limiter.PolicyName("search")
	assert.Equal(t, "trade-search-request-limit", name)
}

func TestZeroTTLDisablesCache(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: 2}, searchOK(t))
	cfg := tradeConfig(m)
	cfg.CacheTTL = 0
	c := newClient(t, cfg)
	_, err := c.search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	_, err = c.search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
}

func TestConcurrentMissesShareOneRequest(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Delay: 100 * time.Millisecond, Expect: 1}, searchOK(t))
	c := newClient(t, tradeConfig(m))
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			_, err := c.search(t.Context(), "Standard", testQuery())
			errs <- err
		}()
	}
	for range 4 {
		require.NoError(t, <-errs)
	}
}

func TestLeagueWithSpacesIsPercentEncoded(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Path: "/api/trade2/search/poe2/Forbidden%20Rites", Expect: 1}, searchOK(t))
	c := newClient(t, tradeConfig(m))
	r, err := c.search(t.Context(), "Forbidden Rites", testQuery())
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%s/trade2/search/poe2/Forbidden%%20Rites/%s", m.URL(), r.ID), c.tradeURL("Forbidden Rites", r.ID))
}

func TestTradeURLsMatchSiteFormat(t *testing.T) {
	c := newClient(t, DefaultConfig("dev"))
	assert.Equal(t, "https://www.pathofexile.com/trade2/search/poe2/Standard/abc", c.tradeURL("Standard", "abc"))
	assert.Equal(t, "https://www.pathofexile.com/trade2/exchange/poe2/HC%20Runes%20of%20Aldur/xyz",
		c.exchangeURL("HC Runes of Aldur", "xyz"))
}

func TestURLEncoding(t *testing.T) {
	c := newClient(t, Config{BaseURL: mustURL(t, "http://h/base/"), UserAgent: tradeUA})
	u := c.url("api", "a,b c?/%")
	u.RawQuery = url.Values{"query": {"x y*~"}, "realm": {"poe2"}}.Encode()
	assert.Equal(t, "http://h/base/api/a,b%20c%3F%2F%25?query=x+y%2A~&realm=poe2", u.String())
}

func TestFetchChunksIDsByTenAndSkipsNulls(t *testing.T) {
	m := newMock(t)
	ids := make([]string, 12)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%d", i)
	}
	first := strings.Join(ids[:fetchChunk], ",")
	second := strings.Join(ids[fetchChunk:], ",")
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + first,
		Match: matchers(queryParam("query", "Q1"), queryParam("realm", "poe2")), Expect: 1}, fetchOK(t))
	tail := map[string]any{"result": []any{fetchResults(t)[0], nil}}
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + second, Expect: 1}, respondJSON(t, 200, tail))
	c := newClient(t, tradeConfig(m))
	entries, err := c.fetch(t.Context(), ids, "Q1")
	require.NoError(t, err)
	assert.Len(t, entries, 7)
	// cached per chunk
	entries, err = c.fetch(t.Context(), ids, "Q1")
	require.NoError(t, err)
	assert.Len(t, entries, 7)
}

func TestFetchChunksRunConcurrentlyInIDOrder(t *testing.T) {
	m := newMock(t)
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%d", i)
	}
	const delay = 500 * time.Millisecond
	results := fetchResults(t)
	second := map[string]any{"result": []any{results[1]}}
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + strings.Join(ids[:fetchChunk], ","), Delay: delay, Expect: 1},
		fetchOK(t))
	m.mount(mockSpec{Method: "GET", Path: "/api/trade2/fetch/" + strings.Join(ids[fetchChunk:], ","), Delay: delay, Expect: 1},
		respondJSON(t, 200, second))
	cfg := tradeConfig(m)
	cfg.CacheTTL = 0
	synctest.Test(t, func(t *testing.T) {
		c := newClient(t, cfg)
		c.http.Transport = inProcess{m}
		start := time.Now()
		entries, err := c.fetch(t.Context(), ids, "Q1")
		require.NoError(t, err)
		// Sequential chunks would take 2 * delay.
		assert.Equal(t, delay, time.Since(start))
		assert.Len(t, entries, len(results)+1)
		assert.Equal(t, results[1].(map[string]any)["id"], entries[len(entries)-1].ID)
	})
}

func TestSearchListingsMapsFetchEntries(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, searchOK(t))
	m.mount(mockSpec{Method: "GET", Expect: 1}, fetchOK(t))
	cfg := tradeConfig(m)
	cfg.FetchCount = 6
	c := newClient(t, cfg)
	page, err := c.searchListings(t.Context(), "Standard", testQuery())
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

	want, err := os.ReadFile(filepath.Join("testdata", "search_listings.json"))
	require.NoError(t, err)
	got, err := json.Marshal(page.Listings)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func TestEmptySearchSkipsFetch(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, respondJSON(t, 200, map[string]any{"id": "E", "result": []any{}, "total": 0}))
	m.mount(mockSpec{Method: "GET", Expect: 0}, fetchOK(t))
	page, err := newClient(t, tradeConfig(m)).searchListings(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	assert.Equal(t, uint32(0), page.Total)
	assert.Empty(t, page.Listings)
}

func TestListingMappingEdgeCases(t *testing.T) {
	var list resultList[fetchEntry]
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "fetch.json"), &list))
	e := list.Result[0]
	e.Listing.Account.Online = nil
	e.Item = fetchItem{StackSize: new(uint32(7))}
	l, ok := listingFromFetch(&e)
	require.True(t, ok)
	assert.False(t, l.Online)
	assert.Equal(t, uint32(7), *l.StackSize)
	assert.Nil(t, l.ItemLevel)
	e.Listing.Price = nil
	_, ok = listingFromFetch(&e)
	assert.False(t, ok)

	var x exchangeResponse
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "exchange.json"), &x))
	entry := x.Result[0]
	entry.Listing.Offers = slices.Clone(entry.Listing.Offers)
	entry.Listing.Offers[0].Item.Amount = 0
	_, ok = listingFromExchange(&entry)
	assert.False(t, ok)
	entry.Listing.Offers = nil
	_, ok = listingFromExchange(&entry)
	assert.False(t, ok)
}

func TestTooManyRequestsMapsRetryAfterAndBlocksFurtherCalls(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: 1}, withHeaders(t, respond(429, []byte("{}")), "search.429"))
	c := newClient(t, tradeConfig(m))
	_, err := c.search(t.Context(), "Standard", testQuery())
	te := asTradeErr(t, err)
	require.Equal(t, KindRateLimited, te.Kind, "expected rate limit, got %v", err)
	assert.Greater(t, te.RetryAfter, 59*time.Second)
	assert.LessOrEqual(t, te.RetryAfter, 60*time.Second)
	info := te.Info()
	assert.Equal(t, "rate_limited", info.Kind)
	assert.Greater(t, *info.RetryAfterMS, uint64(59_000))
	// the limiter now refuses locally without touching the server (Expect: 1 above)
	_, err = c.search(t.Context(), "Standard", testQuery())
	assert.Equal(t, KindRateLimited, asTradeErr(t, err).Kind)
}

func TestRateLimitHeadersThrottleSubsequentCalls(t *testing.T) {
	m := newMock(t)
	resp := searchOK(t)
	resp.Header.Set("X-Rate-Limit-Ip", "1:60:60")
	resp.Header.Set("X-Rate-Limit-Ip-State", "1:60:0")
	m.mount(mockSpec{Method: "POST", Expect: 1}, resp)
	c := newClient(t, tradeConfig(m))
	_, err := c.search(t.Context(), "Standard", testQuery())
	require.NoError(t, err)
	// A different query is not cached; the 1-per-60s rule makes it wait ~60s, which is
	// beyond maxQueueWait, so it fails fast without a request.
	_, err = c.search(t.Context(), "Standard", otherQuery())
	te := asTradeErr(t, err)
	require.Equal(t, KindRateLimited, te.Kind)
	assert.Greater(t, te.RetryAfter, 50*time.Second)
	// Refusals record nothing: retrying does not push the next free slot further out.
	for range 3 {
		_, err := c.search(t.Context(), "Standard", otherQuery())
		again := asTradeErr(t, err)
		require.Equal(t, KindRateLimited, again.Kind)
		assert.LessOrEqual(t, again.RetryAfter, te.RetryAfter)
	}
}

func TestCancelledWaitsReleaseTheirSlot(t *testing.T) {
	m := newMock(t)
	resp := searchOK(t)
	resp.Header.Set("X-Rate-Limit-Ip", "1:2:0")
	resp.Header.Set("X-Rate-Limit-Ip-State", "1:2:0")
	m.mount(mockSpec{Method: "POST", Expect: 1}, resp)
	cfg := tradeConfig(m)
	synctest.Test(t, func(t *testing.T) {
		cfg.CacheTTL = 0
		c := newClient(t, cfg)
		c.http.Transport = inProcess{m}
		_, err := c.search(t.Context(), "Standard", testQuery())
		require.NoError(t, err)
		// The caller gives up while the request is queued behind the limiter.
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, err = c.search(ctx, "Standard", otherQuery())
		require.ErrorIs(t, err, context.DeadlineExceeded, "request should still have been waiting")
		// Its slot was handed back: the next request waits for what is left of one padded
		// window (2.25s), not two.
		_, next, ok := c.limiter.Reserve("search", 0)
		require.False(t, ok)
		assert.Equal(t, 2_250*time.Millisecond-50*time.Millisecond, next)
	})
}

func TestDataEndpointsAreThrottledBeforeAnyHeaders(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/stats", Expect: anyHits}, respond(200, fixtureBytes(t, "stats.sample.json")))
	cfg := tradeConfig(m)
	cfg.CacheTTL = 0
	c := newClient(t, cfg)
	for range 5 {
		_, err := c.dataBody(t.Context(), "stats")
		require.NoError(t, err)
	}
	// Seeded 5:10 rule: the sixth call inside 10s would queue ~10.5s > maxQueueWait.
	_, err := c.dataBody(t.Context(), "stats")
	assert.Equal(t, KindRateLimited, asTradeErr(t, err).Kind)
}

func TestShortWaitsAreSleptThrough(t *testing.T) {
	m := newMock(t)
	resp := searchOK(t)
	resp.Header.Set("X-Rate-Limit-Ip", "1:1:0")
	resp.Header.Set("X-Rate-Limit-Ip-State", "1:1:0")
	m.mount(mockSpec{Method: "POST", Expect: 2}, resp)
	cfg := tradeConfig(m)
	cfg.CacheTTL = 0
	synctest.Test(t, func(t *testing.T) {
		c := newClient(t, cfg)
		c.http.Transport = inProcess{m}
		_, err := c.search(t.Context(), "Standard", testQuery())
		require.NoError(t, err)
		start := time.Now()
		_, err = c.search(t.Context(), "Standard", otherQuery())
		require.NoError(t, err)
		// One second plus the limiter's 250ms edge margin.
		assert.Equal(t, 1_250*time.Millisecond, time.Since(start))
	})
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
	assert.Equal(t, http.StatusServiceUnavailable, te.Status)
	assert.Equal(t, "Maintenance", te.Message)
	assert.Equal(t, "http", te.Info().Kind)
	_, err = c.dataBody(t.Context(), "stats")
	te = asTradeErr(t, err)
	assert.Equal(t, KindHTTP, te.Kind)
	assert.Equal(t, http.StatusInternalServerError, te.Status)
	assert.Equal(t, "upstream exploded", te.Message)
}

func TestInvalidJSONMapsToDecode(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, respond(200, []byte("<html>")))
	m.mount(mockSpec{Path: "/api/trade2/data/leagues", Expect: anyHits}, respondJSON(t, 200, map[string]any{"result": 5}))
	c := newClient(t, tradeConfig(m))
	_, err := c.search(t.Context(), "Standard", testQuery())
	te := asTradeErr(t, err)
	assert.Equal(t, KindDecode, te.Kind)
	assert.Equal(t, "decode", te.Info().Kind)
	_, err = c.Leagues(t.Context())
	assert.Equal(t, KindDecode, asTradeErr(t, err).Kind)
}

func TestOversizedBodiesAreCut(t *testing.T) {
	m := newMock(t)
	huge := append([]byte(`{"result":[`), make([]byte, maxBodySize)...)
	m.mount(mockSpec{Path: "/api/trade2/data/leagues", Expect: anyHits}, respond(200, huge))
	_, err := newClient(t, tradeConfig(m)).Leagues(t.Context())
	assert.Equal(t, KindDecode, asTradeErr(t, err).Kind)
}

func TestTransportErrorsAreReported(t *testing.T) {
	// A server that hangs up on every connection.
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
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
	cfg := DefaultConfig("dev")
	cfg.BaseURL = mustURL(t, "http://"+l.Addr().String())
	c := newClient(t, cfg)
	_, err = c.Leagues(t.Context())
	te := asTradeErr(t, err)
	assert.Equal(t, KindTransport, te.Kind, "%v", err)
	assert.Equal(t, "transport", te.Info().Kind)
}

func TestExchangeBodyShape(t *testing.T) {
	b, err := json.Marshal(exchangeBody("divine", core.StatusSecurable))
	require.NoError(t, err)
	assert.JSONEq(t, `{"query":{"status":{"option":"online"},"have":["exalted"],"want":["divine"]},"sort":{"have":"asc"},"engine":"new"}`, string(b))
	chaos := exchangeBody("chaos", core.StatusAny)
	assert.Equal(t, []string{"exalted", "divine"}, chaos.Query.Have)
	assert.Equal(t, "any", chaos.Query.Status.Option)
	assert.Equal(t, []string{"divine"}, exchangeBody("exalted", core.StatusOnline).Query.Have)
}

func TestExchangeRouteResolvesStaticIDAndMapsOffers(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/static", Expect: 1}, respond(200, fixtureBytes(t, "static.sample.json")))
	body, err := json.Marshal(exchangeBody("transmute", core.StatusOnline))
	require.NoError(t, err)
	m.mount(mockSpec{Method: "POST", Path: "/api/trade2/exchange/poe2/Standard", Match: matchers(bodyJSON(t, body)), Expect: 1},
		withHeaders(t, respond(200, fixtureBytes(t, "exchange.json")), "exchange.200"))
	c := newClient(t, tradeConfig(m))
	f := testFilters(core.RouteExchange, "Orb of Transmutation")
	page, err := c.listingsFor(t.Context(), &f)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), page.Total)
	require.Len(t, page.Listings, 3)
	assert.Equal(t, "exalted", page.Listings[0].Price.Currency)
	assert.Equal(t, uint32(2), *page.Listings[0].StackSize)
	assert.False(t, page.Listings[0].Online)
	assert.True(t, page.Listings[1].Online)
	assert.Contains(t, page.TradeURL, "/trade2/exchange/poe2/Standard/")
	// cached: second lookup hits neither endpoint again
	again, err := c.listingsFor(t.Context(), &f)
	require.NoError(t, err)
	assert.Equal(t, page, again)
	name, _ := c.limiter.PolicyName("exchange")
	assert.Equal(t, "trade-exchange-request-limit", name)
}

func TestExchangeRouteHandlesEmptyAndUnknown(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Path: "/api/trade2/data/static", Expect: anyHits}, respond(200, fixtureBytes(t, "static.sample.json")))
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, respond(200, fixtureBytes(t, "exchange.empty.json")))
	c := newClient(t, tradeConfig(m))
	f := testFilters(core.RouteExchange, "Scroll of Wisdom")
	page, err := c.listingsFor(t.Context(), &f)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), page.Total)
	assert.Empty(t, page.Listings)
	f = testFilters(core.RouteExchange, "Mirror of Nothing")
	_, err = c.listingsFor(t.Context(), &f)
	assert.Equal(t, KindUnsupported, asTradeErr(t, err).Kind)
	id, ok, err := c.staticID(t.Context(), "Orb of Transmutation")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "transmute", id)
}

func TestClientIsAPriceSourceForEverything(t *testing.T) {
	c := newClient(t, DefaultConfig("dev"))
	var src PriceSource = c
	assert.Equal(t, core.SourceTrade, src.Kind())
	assert.True(t, src.Supports(&core.ParsedItem{}))
	assert.Equal(t, 20, c.Config().FetchCount)
}

// Full Price() path: FilterSet.ToTradeQuery + search + fetch + core.Summarise.
func TestPriceFullSearchPath(t *testing.T) {
	m := newMock(t)
	m.mount(mockSpec{Method: "POST", Expect: anyHits}, searchOK(t))
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
	var data staticData
	require.NoError(t, json.Unmarshal([]byte(`{"result": [{"id": "Currency", "entries": [
		{"id": "a", "text": "A", "image": "/gen/a.png"},
		{"id": "b", "text": "B"},
		{"id": "c", "image": "https://cdn.example/c.png"},
		{"id": "a", "text": "dup", "image": "/gen/x.png"}
	]}]}`), &data))
	icons := currencyIcons(data.entries())
	require.Len(t, icons, 2)
	assert.Equal(t, "https://web.poecdn.com/gen/a.png", icons[0].Icon)
	assert.Equal(t, [2]string{"c", "https://cdn.example/c.png"}, [2]string{icons[1].Name, icons[1].Icon})
}
