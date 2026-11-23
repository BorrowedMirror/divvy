//go:build e2e

package e2e

// A programmable trade2 API for the app under test (the Go port of the old
// e2e/mock-trade/server.ts). The app's trade, poe2scout and poe.ninja base URLs all point here,
// so no test ever reaches a real service: the trade routes answer from the configured listings,
// everything else is a recorded 404.
//
//	GET  /api/trade2/data/leagues
//	POST /api/trade2/search/poe2/:league
//	GET  /api/trade2/fetch/:ids?query=:id

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// mockListing is one listing the mock serves.
type mockListing struct {
	Amount   float64
	Currency string
	Account  string // default seller#000n
	Offline  bool
}

// mockConfig is what the mock serves. Total defaults to the number of listings.
type mockConfig struct {
	Leagues  []string
	Listings []mockListing
	Total    int
}

// mockEndpoint is a trade2 route a 429 can be injected on.
type mockEndpoint string

const (
	endpointLeagues mockEndpoint = "leagues"
	endpointSearch  mockEndpoint = "search"
	endpointFetch   mockEndpoint = "fetch"
)

// recordedRequest is one request the mock received (control calls are not recorded).
type recordedRequest struct {
	Method string
	Path   string // path and query
	Body   string
}

type injection struct {
	endpoint   mockEndpoint
	remaining  int
	retryAfter int
}

// mockTrade is the mock trade2 server.
type mockTrade struct {
	srv *httptest.Server

	mu         sync.Mutex
	cfg        mockConfig
	injections []*injection
	requests   []recordedRequest
}

var (
	searchRoute = regexp.MustCompile(`^/api/trade2/search/poe2/[^/]+$`)
	fetchRoute  = regexp.MustCompile(`^/api/trade2/fetch/[^/]+$`)
)

// defaultMockConfig serves Standard and Hardcore and three listings, 1, 2 and 3 divine
// (median 2 divine).
func defaultMockConfig() mockConfig {
	return mockConfig{
		Leagues: []string{"Standard", "Hardcore"},
		Listings: []mockListing{
			{Amount: 1, Currency: "divine"},
			{Amount: 2, Currency: "divine"},
			{Amount: 3, Currency: "divine"},
		},
	}
}

// newMockTrade starts the mock on a free loopback port.
func newMockTrade() *mockTrade {
	m := &mockTrade{cfg: defaultMockConfig()}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// URL is the base URL, e.g. http://127.0.0.1:41234.
func (m *mockTrade) URL() string { return m.srv.URL }

// Close stops the server.
func (m *mockTrade) Close() { m.srv.Close() }

// Reset restores the default config and forgets injections and recorded requests.
func (m *mockTrade) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = defaultMockConfig()
	m.injections = nil
	m.requests = nil
}

// Configure replaces the listings and total; leagues are kept unless cfg sets them.
func (m *mockTrade) Configure(cfg mockConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg.Leagues == nil {
		cfg.Leagues = m.cfg.Leagues
	}
	m.cfg = cfg
}

// RateLimit makes the next `times` requests to endpoint fail with 429 and Retry-After.
func (m *mockTrade) RateLimit(endpoint mockEndpoint, times, retryAfterSecs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injections = append(m.injections, &injection{endpoint: endpoint, remaining: times, retryAfter: retryAfterSecs})
}

// Requests returns a copy of the recorded requests.
func (m *mockTrade) Requests() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedRequest(nil), m.requests...)
}

// Hit reports whether a request with method and a path starting with prefix was received.
func (m *mockTrade) Hit(method, prefix string) bool {
	for _, r := range m.Requests() {
		if r.Method == method && strings.HasPrefix(r.Path, prefix) {
			return true
		}
	}
	return false
}

// listingID is the id of the i-th listing (0-based): 64 hex digits, like trade2's.
func listingID(i int) string { return fmt.Sprintf("%064x", i+1) }

func (m *mockTrade) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path
	pathAndQuery := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		pathAndQuery += "?" + r.URL.RawQuery
	}

	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest{Method: r.Method, Path: pathAndQuery, Body: string(body)})
	var endpoint mockEndpoint
	switch {
	case r.Method == http.MethodGet && path == "/api/trade2/data/leagues":
		endpoint = endpointLeagues
	case r.Method == http.MethodPost && searchRoute.MatchString(path):
		endpoint = endpointSearch
	case r.Method == http.MethodGet && fetchRoute.MatchString(path):
		endpoint = endpointFetch
	}
	var inj *injection
	if endpoint != "" {
		for _, i := range m.injections {
			if i.endpoint == endpoint && i.remaining > 0 {
				i.remaining--
				inj = i
				break
			}
		}
	}
	cfg := m.cfg
	m.mu.Unlock()

	if endpoint == "" {
		send(w, http.StatusNotFound, map[string]any{"error": map[string]any{"code": 1, "message": "Resource not found"}}, nil)
		return
	}
	if inj != nil {
		send(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": 3, "message": "Rate limit exceeded"}},
			rateHeaders("6:10:60,6:60:0", map[string]string{"Retry-After": strconv.Itoa(inj.retryAfter)}))
		return
	}
	ok := func(v any) { send(w, http.StatusOK, v, rateHeaders("1:10:0,1:60:0", nil)) }
	switch endpoint {
	case endpointLeagues:
		leagues := make([]map[string]string, 0, len(cfg.Leagues))
		for _, id := range cfg.Leagues {
			leagues = append(leagues, map[string]string{"id": id, "realm": "poe2", "text": id})
		}
		ok(map[string]any{"result": leagues})
	case endpointSearch:
		ids := make([]string, len(cfg.Listings))
		for i := range cfg.Listings {
			ids[i] = listingID(i)
		}
		total := cfg.Total
		if total == 0 {
			total = len(cfg.Listings)
		}
		ok(map[string]any{"id": "divvyMock", "complexity": 4, "result": ids, "total": total})
	case endpointFetch:
		raw, _ := url.PathUnescape(strings.TrimPrefix(path, "/api/trade2/fetch/"))
		result := []any{}
		for id := range strings.SplitSeq(raw, ",") {
			if id == "" {
				continue
			}
			result = append(result, fetchResult(cfg, id))
		}
		ok(map[string]any{"result": result})
	}
}

// fetchResult is one trade2 fetch entry for listing id, or nil when there is none.
func fetchResult(cfg mockConfig, id string) any {
	for i, l := range cfg.Listings {
		if listingID(i) != id {
			continue
		}
		account := l.Account
		if account == "" {
			account = fmt.Sprintf("seller#%04d", i+1)
		}
		var online any
		if !l.Offline && len(cfg.Leagues) > 0 {
			online = map[string]string{"league": cfg.Leagues[0]}
		}
		return map[string]any{
			"id": id,
			"listing": map[string]any{
				"method":  "psapi",
				"indexed": "2026-11-01T10:00:00Z",
				"account": map[string]any{"name": account, "online": online, "lastCharacterName": "MockCharacter"},
				"price":   map[string]any{"type": "~price", "amount": l.Amount, "currency": l.Currency},
			},
			"item": map[string]any{"name": "", "typeLine": "Mock Item", "baseType": "Mock Item", "frameType": 0},
		}
	}
	return nil
}

func rateHeaders(state string, extra map[string]string) map[string]string {
	h := map[string]string{
		"X-Rate-Limit-Policy":   "trade-search-request-limit",
		"X-Rate-Limit-Rules":    "Ip",
		"X-Rate-Limit-Ip":       "5:10:60,15:60:300",
		"X-Rate-Limit-Ip-State": state,
	}
	maps.Copy(h, extra)
	return h
}

func send(w http.ResponseWriter, status int, body any, headers map[string]string) {
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
