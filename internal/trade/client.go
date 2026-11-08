package trade

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maypok86/otter/v2"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade/ratelimit"
)

// Config configures a [Client].
type Config struct {
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

// DefaultConfig is the production configuration for the given application version.
func DefaultConfig(version string) Config {
	return Config{
		BaseURL:    &url.URL{Scheme: "https", Host: "www.pathofexile.com"},
		UserAgent:  DefaultUserAgent(version),
		FetchCount: 20,
		CacheTTL:   5 * time.Minute,
	}
}

// Rate-limiter policy keys. Each maps to one GGG policy.
const (
	policySearch   = "search"
	policyFetch    = "fetch"
	policyExchange = "exchange"
	policyData     = "data"
)

// maxQueueWait is the longest a request is queued behind the limiter before giving up with
// a rate-limited error (an overlay waiting longer than this is useless).
const maxQueueWait = 10 * time.Second

// Anonymous limits observed on 2026-11-05 (docs/research/trade2.md), replaced by live
// headers after the first response. Data endpoints are CDN-cached and send no headers; stay
// conservative anyway.
var seedRules = map[string]string{
	policySearch:   "5:10:60,15:60:300,30:300:1800",
	policyFetch:    "12:4:10,16:12:300,50:300:300",
	policyExchange: "5:15:60,10:90:300,30:300:1800",
	policyData:     "5:10:60",
}

// listingPage is search/fetch results mapped to core listings, plus the link to the same
// search.
type listingPage struct {
	SearchID string
	Total    uint32
	Listings []core.Listing
	TradeURL string
}

// Client is the official trade2 API client. Safe for concurrent use.
//
// Responses are cached (see [Config.CacheTTL]); cached values are shared and never
// modified.
type Client struct {
	config    Config
	cookie    string
	http      *http.Client
	limiter   *ratelimit.Limiter
	searches  *otter.Cache[string, *searchResponse]
	fetches   *otter.Cache[string, []fetchEntry]
	exchanges *otter.Cache[string, *exchangeResponse]
	// data holds raw `data/*` bodies by endpoint name.
	data *otter.Cache[string, []byte]
}

// validHeaderValue reports whether s can be sent as an HTTP header value.
func validHeaderValue(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return (r < 0x20 && r != '\t') || r == 0x7f })
}

// NewClient validates config and builds a client.
func NewClient(config Config) (*Client, error) {
	if err := checkBaseURL(config.BaseURL); err != nil {
		return nil, err
	}
	var cookie string
	if config.POESESSID != "" {
		cookie = "POESESSID=" + config.POESESSID
		if !validHeaderValue(cookie) {
			return nil, configError("invalid POESESSID: not a valid header value")
		}
	}
	limiter := ratelimit.New()
	for policy, spec := range seedRules {
		rules, err := ratelimit.ParseRules(spec)
		if err != nil {
			return nil, configError("seed rules for %s: %v", policy, err)
		}
		limiter.Seed(policy, rules)
	}
	ttl := config.CacheTTL
	return &Client{
		config:    config,
		cookie:    cookie,
		http:      newHTTPClient(),
		limiter:   limiter,
		searches:  newCache[string, *searchResponse](512, ttl),
		fetches:   newCache[string, []fetchEntry](512, ttl),
		exchanges: newCache[string, *exchangeResponse](512, ttl),
		data:      newCache[string, []byte](512, ttl),
	}, nil
}

// Config is the client's configuration.
func (c *Client) Config() *Config { return &c.config }

func (c *Client) url(segments ...string) *url.URL { return joinPath(c.config.BaseURL, segments...) }

func (c *Client) request(ctx context.Context, method string, u *url.URL, body []byte) (*http.Request, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(ctx, method, u.String(), nil)
	}
	if err != nil {
		return nil, transportError(err)
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
func (c *Client) send(ctx context.Context, policy string, req *http.Request) ([]byte, error) {
	if blocked := c.limiter.BlockedFor(policy); blocked != 0 {
		return nil, rateLimitedError(blocked)
	}
	// Refusals record nothing; if the caller gives up while waiting, the slot is released
	// so cancelled requests do not throttle later ones.
	slot, refused, ok := c.limiter.Reserve(policy, maxQueueWait)
	if !ok {
		return nil, rateLimitedError(refused)
	}
	defer slot.Release()
	if wait := slot.Wait(); wait != 0 {
		slog.Debug("rate limiter delaying request", "policy", policy, "wait", wait)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
	if err := ctx.Err(); err != nil {
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
		return nil, rateLimitedError(c.limiter.BlockedFor(policy))
	}
	body, err := readBody(ctx, resp)
	if err != nil {
		return nil, err
	}
	if !isSuccess(resp.StatusCode) {
		return nil, httpError(resp.StatusCode, errorMessage(body))
	}
	return body, nil
}

// errorMessage is the `error.message` of a trade2 error body, else the start of the body.
func errorMessage(body []byte) string {
	var e apiError
	if json.Unmarshal(body, &e) == nil && e.Error.Message != nil {
		return *e.Error.Message
	}
	return bodyPrefix(body)
}

// decode decodes a response body into dst.
func decode(body []byte, dst any) error {
	if err := json.Unmarshal(body, dst); err != nil {
		return decodeError("%v", err)
	}
	return nil
}

// Kind implements [PriceSource].
func (c *Client) Kind() core.PriceSourceKind { return core.SourceTrade }

// Supports implements [PriceSource]: the trade site prices everything.
func (c *Client) Supports(*core.ParsedItem) bool { return true }

// Price implements [PriceSource].
func (c *Client) Price(ctx context.Context, req *PriceRequest) (core.PriceSummary, error) {
	page, err := c.listingsFor(ctx, &req.Filters)
	if err != nil {
		return core.PriceSummary{}, err
	}
	summary := core.Summarise(core.SourceTrade, page.Total, page.Listings, nil)
	summary.TradeURL = &page.TradeURL
	return summary, nil
}

// listingsFor runs the filters against the right route and returns raw listings.
func (c *Client) listingsFor(ctx context.Context, filters *core.FilterSet) (*listingPage, error) {
	if filters.Route == core.RouteExchange {
		var name string
		switch {
		case filters.BaseType != nil:
			name = *filters.BaseType
		case filters.Name != nil:
			name = *filters.Name
		}
		want, ok, err := c.staticID(ctx, name)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, unsupportedError("trade exchange")
		}
		return c.exchangeListings(ctx, filters.League, want, filters.Status)
	}
	q := filters.ToTradeQuery()
	return c.searchListings(ctx, filters.League, &q)
}

// sellerOf is the account name of a listing's seller, if known. Listings point into the
// (immutable) responses they were mapped from.
func sellerOf(a *fetchAccount) *string {
	if a == nil {
		return nil
	}
	return &a.Name
}
