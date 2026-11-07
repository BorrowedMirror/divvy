package trade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maypok86/otter/v2"
	"golang.org/x/sync/errgroup"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// TradeConfig configures a [TradeClient].
type TradeConfig struct {
	// BaseURL is `https://www.pathofexile.com` in production; a mock server in tests.
	BaseURL *url.URL
	// UserAgent is sent with every request.
	UserAgent string
	// POESESSID is the optional POESESSID cookie value ("" = none).
	POESESSID string
	// FetchCount is the max listings to fetch per search (fetch takes <= 10 ids per call).
	FetchCount int
	// CacheTTL is how long responses are cached; 0 disables the cache.
	CacheTTL time.Duration
}

// DefaultTradeConfig is the production configuration.
func DefaultTradeConfig() TradeConfig {
	u, _ := url.Parse("https://www.pathofexile.com")
	return TradeConfig{
		BaseURL:    u,
		UserAgent:  DefaultUserAgent(),
		FetchCount: 20,
		CacheTTL:   300 * time.Second,
	}
}

// Rate-limiter keys. Each maps to one GGG policy.
const (
	PolicySearch   = "search"
	PolicyFetch    = "fetch"
	PolicyExchange = "exchange"
	PolicyData     = "data"
)

// FetchChunk is the max ids per `fetch` call (server limit).
const FetchChunk = 10

// MaxQueueWait is the longest a request is queued behind the limiter before giving up with a
// rate-limited error (an overlay waiting longer than this is useless).
const MaxQueueWait = 10 * time.Second

// ListingPage is search/fetch results mapped to core listings, plus the link to the same
// search.
type ListingPage struct {
	SearchID string
	Total    uint32
	Listings []core.Listing
	TradeURL string
}

// TradeClient is the official trade2 API client. Safe for concurrent use.
type TradeClient struct {
	config    TradeConfig
	cookie    string
	http      *http.Client
	limiter   *RateLimiter
	searches  *otter.Cache[string, *SearchResponse]
	fetches   *otter.Cache[string, []FetchEntry]
	exchanges *otter.Cache[string, *ExchangeResponse]
	data      *otter.Cache[string, any]
}

func mustRules(spec string) []Rule {
	r, err := ParseRules(spec)
	if err != nil {
		panic("static rules: " + err.Error())
	}
	return r
}

// validHeaderValue is http's HeaderValue::from_str check.
func validHeaderValue(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

// NewTradeClient validates config and builds a client.
func NewTradeClient(config TradeConfig) (*TradeClient, error) {
	if cannotBeABase(config.BaseURL) {
		shown := "<nil>"
		if config.BaseURL != nil {
			shown = config.BaseURL.String()
		}
		return nil, ConfigError(fmt.Sprintf("base_url `%s` cannot be a base", shown))
	}
	var cookie string
	if config.POESESSID != "" {
		cookie = "POESESSID=" + config.POESESSID
		if !validHeaderValue(cookie) {
			return nil, ConfigError("invalid POESESSID: failed to parse header value")
		}
	}
	limiter := NewRateLimiter()
	// Anonymous limits observed on 2026-11-05 (docs/research/trade2.md); replaced by live
	// headers after the first response.
	limiter.Seed(PolicySearch, mustRules("5:10:60,15:60:300,30:300:1800"))
	limiter.Seed(PolicyFetch, mustRules("12:4:10,16:12:300,50:300:300"))
	limiter.Seed(PolicyExchange, mustRules("5:15:60,10:90:300,30:300:1800"))
	// Data endpoints are CDN-cached and send no headers; stay conservative anyway.
	limiter.Seed(PolicyData, mustRules("5:10:60"))
	ttl := config.CacheTTL
	return &TradeClient{
		config:    config,
		cookie:    cookie,
		http:      newHTTPClient(20 * time.Second),
		limiter:   limiter,
		searches:  newCache[string, *SearchResponse](512, ttl),
		fetches:   newCache[string, []FetchEntry](512, ttl),
		exchanges: newCache[string, *ExchangeResponse](512, ttl),
		data:      newCache[string, any](512, ttl),
	}, nil
}

// Config is the client's configuration.
func (c *TradeClient) Config() *TradeConfig { return &c.config }

// Limiter is the rate limiter, for diagnostics.
func (c *TradeClient) Limiter() *RateLimiter { return c.limiter }

func (c *TradeClient) url(segments ...string) *url.URL {
	return appendSegments(c.config.BaseURL, segments...)
}

func (c *TradeClient) request(ctx context.Context, method string, u *url.URL, body []byte) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, TransportError(err)
	}
	req.Header.Set("User-Agent", c.config.UserAgent)
	req.Header.Set("Accept", "application/json")
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// send runs req under the limiter policy and returns the successful response body.
func (c *TradeClient) send(ctx context.Context, policy string, req *http.Request) ([]byte, error) {
	if blocked := c.limiter.BlockedFor(policy); blocked != 0 {
		return nil, RateLimitedError(blocked)
	}
	// Refusals record nothing; if the caller gives up while waiting, the slot is released
	// so cancelled requests do not throttle later ones.
	slot, refused, ok := c.limiter.ReserveGuarded(policy, MaxQueueWait)
	if !ok {
		return nil, RateLimitedError(refused)
	}
	if wait := slot.Wait(); wait != 0 {
		slog.Debug("rate limiter delaying request", "policy", policy, "wait", wait)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			slot.Release()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
	if err := ctx.Err(); err != nil {
		slot.Release()
		return nil, err
	}
	slot.Commit()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	c.limiter.Observe(policy, resp.StatusCode, resp.Header)
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, RateLimitedError(c.limiter.BlockedFor(policy))
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, transportErr(ctx, err)
	}
	body := buf.Bytes()
	if !isSuccess(resp.StatusCode) {
		return nil, HTTPError(uint16(resp.StatusCode), errorMessage(body))
	}
	return body, nil
}

func decodeInto(body []byte, dst any) error {
	if err := json.Unmarshal(body, dst); err != nil {
		return DecodeError(err.Error())
	}
	return nil
}

// Leagues is `GET /api/trade2/data/leagues`.
func (c *TradeClient) Leagues(ctx context.Context) ([]League, error) {
	v, err := c.dataCached(ctx, "leagues")
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, DecodeError(err.Error())
	}
	var list ResultList[League]
	if err := decodeInto(raw, &list); err != nil {
		return nil, err
	}
	return list.Result, nil
}

// Data is `GET /api/trade2/data/{stats|items|static|filters}` raw JSON (a serde_json::Value
// equivalent: numbers are json.Number). The result is the caller's own copy.
func (c *TradeClient) Data(ctx context.Context, which string) (any, error) {
	v, err := c.dataCached(ctx, which)
	if err != nil {
		return nil, err
	}
	return cloneValue(v), nil
}

// dataCached returns the shared cached value; callers must not modify it.
func (c *TradeClient) dataCached(ctx context.Context, which string) (any, error) {
	if hit, ok := cacheGet(c.data, which); ok {
		return hit, nil
	}
	req, err := c.request(ctx, http.MethodGet, c.url("api", "trade2", "data", which), nil)
	if err != nil {
		return nil, err
	}
	body, err := c.send(ctx, PolicyData, req)
	if err != nil {
		return nil, err
	}
	v, err := decodeValue(body)
	if err != nil {
		return nil, DecodeError(err.Error())
	}
	cacheSet(c.data, which, v)
	return v, nil
}

// Search is `POST /api/trade2/search/poe2/{league}`.
func (c *TradeClient) Search(ctx context.Context, league string, query *core.TradeQuery) (*SearchResponse, error) {
	body, err := core.MarshalSerde(query)
	if err != nil {
		return nil, DecodeError(err.Error())
	}
	key := league + "\n" + string(body)
	if hit, ok := cacheGet(c.searches, key); ok {
		return hit.clone(), nil
	}
	req, err := c.request(ctx, http.MethodPost, c.url("api", "trade2", "search", "poe2", league), body)
	if err != nil {
		return nil, err
	}
	raw, err := c.send(ctx, PolicySearch, req)
	if err != nil {
		return nil, err
	}
	var resp SearchResponse
	if err := decodeInto(raw, &resp); err != nil {
		return nil, err
	}
	cacheSet(c.searches, key, resp.clone())
	return &resp, nil
}

// Fetch is `GET /api/trade2/fetch/{id,id,...}?query={searchID}`, in chunks of [FetchChunk].
// Listings that vanished between search and fetch (`null`) are skipped.
//
// Chunks are requested concurrently (each still takes its own rate-limiter slot), so the
// default 20 listings cost one round trip instead of two; results keep id order. The first
// failing chunk cancels the others.
func (c *TradeClient) Fetch(ctx context.Context, ids []string, searchID string) ([]FetchEntry, error) {
	var chunks [][]string
	for i := 0; i < len(ids); i += FetchChunk {
		chunks = append(chunks, ids[i:min(i+FetchChunk, len(ids))])
	}
	results := make([][]FetchEntry, len(chunks))
	g, gctx := errgroup.WithContext(ctx)
	for i, chunk := range chunks {
		g.Go(func() error {
			r, err := c.fetchChunk(gctx, chunk, searchID)
			results[i] = r
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make([]FetchEntry, 0, len(ids))
	for _, r := range results {
		out = append(out, r...)
	}
	return out, nil
}

func (c *TradeClient) fetchChunk(ctx context.Context, chunk []string, searchID string) ([]FetchEntry, error) {
	joined := strings.Join(chunk, ",")
	key := searchID + "\n" + joined
	if hit, ok := cacheGet(c.fetches, key); ok {
		return hit, nil
	}
	u := c.url("api", "trade2", "fetch", joined)
	appendQuery(u, [2]string{"query", searchID}, [2]string{"realm", "poe2"})
	req, err := c.request(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	raw, err := c.send(ctx, PolicyFetch, req)
	if err != nil {
		return nil, err
	}
	var list ResultList[*FetchEntry]
	if err := decodeInto(raw, &list); err != nil {
		return nil, err
	}
	entries := make([]FetchEntry, 0, len(list.Result))
	for _, e := range list.Result {
		if e != nil {
			entries = append(entries, *e)
		}
	}
	cacheSet(c.fetches, key, entries)
	return entries, nil
}

// Exchange is `POST /api/trade2/exchange/poe2/{league}` with a raw bulk-exchange body (a
// serde_json::Value equivalent, written with sorted keys).
func (c *TradeClient) Exchange(ctx context.Context, league string, body any) (*ExchangeResponse, error) {
	b, err := core.MarshalSerde(body)
	if err != nil {
		return nil, DecodeError(err.Error())
	}
	key := league + "\n" + string(b)
	if hit, ok := cacheGet(c.exchanges, key); ok {
		return hit.clone(), nil
	}
	req, err := c.request(ctx, http.MethodPost, c.url("api", "trade2", "exchange", "poe2", league), b)
	if err != nil {
		return nil, err
	}
	raw, err := c.send(ctx, PolicyExchange, req)
	if err != nil {
		return nil, err
	}
	var resp ExchangeResponse
	if err := decodeInto(raw, &resp); err != nil {
		return nil, err
	}
	cacheSet(c.exchanges, key, resp.clone())
	return &resp, nil
}

// TradeURL is `https://www.pathofexile.com/trade2/search/poe2/{league}/{searchID}`.
func (c *TradeClient) TradeURL(league, searchID string) string {
	return c.url("trade2", "search", "poe2", league, searchID).String()
}

// ExchangeURL is `https://www.pathofexile.com/trade2/exchange/poe2/{league}/{searchID}`.
func (c *TradeClient) ExchangeURL(league, searchID string) string {
	return c.url("trade2", "exchange", "poe2", league, searchID).String()
}

// SearchListings searches, then fetches the first FetchCount results and maps them to
// listings.
func (c *TradeClient) SearchListings(ctx context.Context, league string, query *core.TradeQuery) (*ListingPage, error) {
	search, err := c.Search(ctx, league, query)
	if err != nil {
		return nil, err
	}
	ids := search.Result[:min(len(search.Result), max(c.config.FetchCount, 0))]
	var entries []FetchEntry
	if len(ids) > 0 {
		if entries, err = c.Fetch(ctx, ids, search.ID); err != nil {
			return nil, err
		}
	}
	listings := []core.Listing{}
	for i := range entries {
		if l, ok := ListingFromFetch(&entries[i]); ok {
			listings = append(listings, l)
		}
	}
	return &ListingPage{
		TradeURL: c.TradeURL(league, search.ID),
		SearchID: search.ID,
		Total:    search.Total,
		Listings: listings,
	}, nil
}

// CurrencyIcons lists currency icons from `data/static`: every exchange tag that has an
// image, with its name and the absolute image URL on GGG's CDN (the URLs the official trade
// site uses; the images are not bundled).
func (c *TradeClient) CurrencyIcons(ctx context.Context) ([]CurrencyIcon, error) {
	v, err := c.dataCached(ctx, "static")
	if err != nil {
		return nil, err
	}
	return CurrencyIconsFrom(v), nil
}

// staticEntries iterates `v["result"][*]["entries"][*]`.
func staticEntries(v any, yield func(e any) bool) {
	for _, g := range asArray(index(v, "result")) {
		for _, e := range asArray(index(g, "entries")) {
			if !yield(e) {
				return
			}
		}
	}
}

// StaticID is the exchange tag (`divine`, `exalted`, ...) for an item name, from
// `data/static`.
func (c *TradeClient) StaticID(ctx context.Context, text string) (string, bool, error) {
	v, err := c.dataCached(ctx, "static")
	if err != nil {
		return "", false, err
	}
	var id string
	var found bool
	staticEntries(v, func(e any) bool {
		if t, ok := asStr(index(e, "text")); ok && t == text {
			id, found = asStr(index(e, "id"))
			return false
		}
		return true
	})
	return id, found, nil
}

// ExchangeListings is a bulk-exchange lookup for an item sold by exchange tag want.
func (c *TradeClient) ExchangeListings(ctx context.Context, league, want string, status core.OnlineStatus) (*ListingPage, error) {
	resp, err := c.Exchange(ctx, league, ExchangeBody(want, status))
	if err != nil {
		return nil, err
	}
	listings := []core.Listing{}
	for i := range resp.Result {
		if len(listings) >= c.config.FetchCount {
			break
		}
		if l, ok := ListingFromExchange(&resp.Result[i]); ok {
			listings = append(listings, l)
		}
	}
	return &ListingPage{
		TradeURL: c.ExchangeURL(league, resp.ID),
		SearchID: resp.ID,
		Total:    resp.Total,
		Listings: listings,
	}, nil
}

// ListingsFor runs the filters against the right route and returns raw listings.
func (c *TradeClient) ListingsFor(ctx context.Context, filters *core.FilterSet) (*ListingPage, error) {
	if filters.Route == core.RouteExchange {
		var name string
		switch {
		case filters.BaseType != nil:
			name = *filters.BaseType
		case filters.Name != nil:
			name = *filters.Name
		}
		want, ok, err := c.StaticID(ctx, name)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, UnsupportedError("trade exchange")
		}
		return c.ExchangeListings(ctx, filters.League, want, filters.Status)
	}
	q := filters.ToTradeQuery()
	return c.SearchListings(ctx, filters.League, &q)
}

// ExchangeBody is the bulk-exchange body, as the trade site sends it. Prices are asked in
// exalted and divine, except for the currency being priced itself.
func ExchangeBody(want string, status core.OnlineStatus) map[string]any {
	have := []any{}
	for _, c := range []string{"exalted", "divine"} {
		if c != want {
			have = append(have, c)
		}
	}
	option := "online"
	if status == core.StatusAny {
		option = "any"
	}
	// The exchange has no "securable" option.
	return map[string]any{
		"query": map[string]any{
			"status": map[string]any{"option": option},
			"have":   have,
			"want":   []any{want},
		},
		"sort":   map[string]any{"have": "asc"},
		"engine": "new",
	}
}

func errorMessage(body []byte) string {
	if v, err := decodeValue(body); err == nil {
		if m, ok := asStr(index(index(v, "error"), "message")); ok {
			return m
		}
	}
	return lossy(body[:min(len(body), 200)])
}

func onlineOf(a *FetchAccount) bool { return a != nil && a.Online != nil }

func sellerOf(a *FetchAccount) *string {
	if a == nil {
		return nil
	}
	s := a.Name
	return &s
}

// ListingFromFetch (Rust `fetch_listing`) maps a fetch entry to a core listing; entries without a price are dropped.
func ListingFromFetch(e *FetchEntry) (core.Listing, bool) {
	price := e.Listing.Price
	if price == nil {
		return core.Listing{}, false
	}
	asU32 := func(k string) *uint32 {
		if v, ok := asU64(index(e.Item, k)); ok {
			u := uint32(v)
			return &u
		}
		return nil
	}
	var whisper *string
	if w := e.Listing.Whisper; w != nil && strings.TrimSpace(*w) != "" {
		s := *w
		whisper = &s
	}
	return core.Listing{
		ID:        e.ID,
		Price:     core.Price{Amount: price.Amount, Currency: price.Currency},
		Seller:    sellerOf(e.Listing.Account),
		Online:    onlineOf(e.Listing.Account),
		Indexed:   clonePtr(e.Listing.Indexed),
		ItemLevel: asU32("ilvl"),
		StackSize: asU32("stackSize"),
		Whisper:   whisper,
	}, true
}

// ListingFromExchange (Rust `exchange_listing`) maps an exchange entry to a per-unit listing; StackSize is the seller's
// stock.
func ListingFromExchange(e *ExchangeEntry) (core.Listing, bool) {
	if len(e.Listing.Offers) == 0 {
		return core.Listing{}, false
	}
	offer := e.Listing.Offers[0]
	if offer.Item.Amount <= 0 {
		return core.Listing{}, false
	}
	return core.Listing{
		ID:        e.ID,
		Price:     core.Price{Amount: offer.Exchange.Amount / offer.Item.Amount, Currency: offer.Exchange.Currency},
		Seller:    sellerOf(e.Listing.Account),
		Online:    onlineOf(e.Listing.Account),
		Indexed:   clonePtr(e.Listing.Indexed),
		StackSize: clonePtr(offer.Item.Stock),
	}, true
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// Kind implements [PriceSource].
func (c *TradeClient) Kind() core.PriceSourceKind { return core.SourceTrade }

// Supports implements [PriceSource]: the trade site prices everything.
func (c *TradeClient) Supports(*core.ParsedItem) bool { return true }

// Price implements [PriceSource].
func (c *TradeClient) Price(ctx context.Context, req *PriceRequest) (core.PriceSummary, error) {
	page, err := c.ListingsFor(ctx, &req.Filters)
	if err != nil {
		return core.PriceSummary{}, err
	}
	summary := core.Summarise(core.SourceTrade, page.Total, page.Listings, nil)
	u := page.TradeURL
	summary.TradeURL = &u
	return summary, nil
}

// ImageCDN is the base for the relative image paths in `data/static`.
const ImageCDN = "https://web.poecdn.com"

// CurrencyIcon is one currency (exchange tag) and its icon.
type CurrencyIcon struct {
	// ID is the exchange tag, as used in listing prices (`divine`, `exalted`, `chaos`, ...).
	ID string `json:"id"`
	// Name is the English item name (`Divine Orb`).
	Name string `json:"name"`
	// Icon is the absolute image URL.
	Icon string `json:"icon"`
}

// MarshalJSON writes serde-compatible JSON.
func (i CurrencyIcon) MarshalJSON() ([]byte, error) {
	type plain CurrencyIcon
	return core.MarshalSerde(plain(i))
}

// UnmarshalJSON decodes like serde.
func (i *CurrencyIcon) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct CurrencyIcon")
	if err != nil {
		return err
	}
	var v CurrencyIcon
	for _, f := range []struct {
		key string
		dst *string
	}{{"id", &v.ID}, {"name", &v.Name}, {"icon", &v.Icon}} {
		if err := o.required(f.key, f.dst); err != nil {
			return err
		}
	}
	*i = v
	return nil
}

// CurrencyIconsFrom extracts [CurrencyIcon]s from a `data/static` response (decoded JSON).
// Entries without an image are skipped; the first entry wins for a repeated id.
func CurrencyIconsFrom(v any) []CurrencyIcon {
	out := []CurrencyIcon{}
	seen := map[string]bool{}
	staticEntries(v, func(e any) bool {
		id, ok1 := asStr(index(e, "id"))
		image, ok2 := asStr(index(e, "image"))
		if !ok1 || !ok2 || image == "" || seen[id] {
			return true
		}
		icon := image
		if !strings.HasPrefix(image, "https://") {
			icon = ImageCDN + image
		}
		name, ok := asStr(index(e, "text"))
		if !ok {
			name = id
		}
		seen[id] = true
		out = append(out, CurrencyIcon{ID: id, Name: name, Icon: icon})
		return true
	})
	return out
}
