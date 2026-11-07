package testkit

// Trade API fixtures and a small programmable HTTP mock (the Go stand-in for wiremock).
//
// Recorded trade2 JSON lives in crates/trade/tests/fixtures/ (owned by the trade crate).
// [Fixture] reads from there first and falls back to testkit's own minimal samples in
// crates/testkit/fixtures/, so testkit (and anything built on it) works even before the recorded
// set exists.
//
//   - [MountTradeFixtures] mounts static leagues / search / fetch responses.
//   - [TradeMock] is a programmable trade2 API: chosen prices, listing counts and 429 injection.

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// API paths of the trade2 API (relative to the trade client's base URL).
const (
	LeaguesPath  = "/api/trade2/data/leagues"
	SearchPrefix = "/api/trade2/search/poe2/"
	FetchPrefix  = "/api/trade2/fetch/"
)

var (
	searchPath = regexp.MustCompile("^" + regexp.QuoteMeta(SearchPrefix) + "[^/]+$")
	fetchPath  = regexp.MustCompile("^" + regexp.QuoteMeta(FetchPrefix) + "[^/]+$")
)

// TradeFixturesDir is the trade crate's recorded fixtures (crates/trade/tests/fixtures).
func TradeFixturesDir() string {
	return filepath.Join(repoRoot(), "crates", "trade", "tests", "fixtures")
}

// TestkitFixturesDir is testkit's own minimal sample fixtures (crates/testkit/fixtures).
func TestkitFixturesDir() string {
	return filepath.Join(repoRoot(), "crates", "testkit", "fixtures")
}

// FixtureErrorKind classifies a [FixtureError].
type FixtureErrorKind int

const (
	FixtureNotFound FixtureErrorKind = iota + 1
	FixtureIO
	FixtureJSON
)

// FixtureError is a fixture that could not be found, read or parsed. NotFound carries Name and
// Searched; IO and JSON carry Path and Err.
type FixtureError struct {
	Kind     FixtureErrorKind
	Name     string
	Searched []string
	Path     string
	Err      error
}

func (e *FixtureError) Error() string {
	switch e.Kind {
	case FixtureNotFound:
		var b strings.Builder
		fmt.Fprintf(&b, "fixture `%s` not found; looked in:", e.Name)
		for _, p := range e.Searched {
			b.WriteString(" " + p)
		}
		return b.String()
	case FixtureIO:
		return fmt.Sprintf("reading fixture %s: %v", e.Path, e.Err)
	default:
		return fmt.Sprintf("fixture %s is not valid JSON: %v", e.Path, e.Err)
	}
}

func (e *FixtureError) Unwrap() error { return e.Err }

// readJSON reads path and checks it is JSON, returning the raw bytes.
func readJSON(path string) (json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, &FixtureError{Kind: FixtureIO, Path: path, Err: err}
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, &FixtureError{Kind: FixtureJSON, Path: path, Err: err}
	}
	return b, nil
}

// TryFixtureIn loads a fixture by name (with or without `.json`) from dirs in order.
func TryFixtureIn(dirs []string, name string) (json.RawMessage, error) {
	file := name
	if filepath.Ext(name) != ".json" {
		file = name + ".json"
	}
	var searched []string
	for _, dir := range dirs {
		p := filepath.Join(dir, file)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return readJSON(p)
		}
		searched = append(searched, p)
	}
	return nil, &FixtureError{Kind: FixtureNotFound, Name: name, Searched: searched}
}

// TryFixture loads crates/trade/tests/fixtures/<name>.json, else
// crates/testkit/fixtures/<name>.json.
func TryFixture(name string) (json.RawMessage, error) {
	return TryFixtureIn([]string{TradeFixturesDir(), TestkitFixturesDir()}, name)
}

// Fixture is [TryFixture], panicking with a readable message (tests only).
func Fixture(name string) json.RawMessage {
	b, err := TryFixture(name)
	if err != nil {
		panic(err.Error())
	}
	return b
}

// TestkitFixture loads one of testkit's own samples (never the recorded set). Stable for
// assertions.
func TestkitFixture(name string) json.RawMessage {
	b, err := TryFixtureIn([]string{TestkitFixturesDir()}, name)
	if err != nil {
		panic(err.Error())
	}
	return b
}

// FixtureHeaders reads a recorded header set, crates/trade/tests/fixtures/headers/<name>.txt
// (`Name: value` per line), as the trade crate's tests do.
func FixtureHeaders(name string) (http.Header, error) {
	b, err := os.ReadFile(filepath.Join(TradeFixturesDir(), "headers", name+".txt"))
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	for line := range strings.Lines(string(b)) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("header fixture %s: bad line %q", name, line)
		}
		h.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	return h, nil
}

// ---------------------------------------------------------------------------------------------
// Mock server
// ---------------------------------------------------------------------------------------------

// Response is what a [Route] answers.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSONResponse is a response with a JSON body (marshalled unless already []byte or
// json.RawMessage) and `Content-Type: application/json`.
func JSONResponse(status int, body any) Response {
	var b []byte
	switch v := body.(type) {
	case json.RawMessage:
		b = v
	case []byte:
		b = v
	default:
		var err error
		if b, err = json.Marshal(v); err != nil {
			panic(fmt.Sprintf("testkit: marshal response: %v", err))
		}
	}
	return Response{Status: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: b}
}

// DefaultPriority is the priority of a route that sets none (wiremock's default).
const DefaultPriority = 5

// Route is one mocked endpoint. A request matches when Method (if set) and Path (exact) or
// PathRegex (if set) match its escaped path.
type Route struct {
	Method    string
	Path      string
	PathRegex *regexp.Regexp
	Respond   func(*http.Request) Response
	// Times limits how many requests the route answers (0 = unlimited), after which it no longer
	// matches.
	Times int
	// Priority picks among matching routes: lowest wins, then the first mounted. 0 means
	// [DefaultPriority].
	Priority int
}

type mountedRoute struct {
	Route
	served int
}

// MockServer is an httptest server answering from mounted routes, 404 when none matches.
type MockServer struct {
	server *httptest.Server
	mu     sync.Mutex
	routes []*mountedRoute
}

// NewMockServer starts a mock server on a random local port. Close it when done.
func NewMockServer() *MockServer {
	s := &MockServer{}
	s.server = httptest.NewServer(s)
	return s
}

// NewMockServerAt starts a mock server listening on addr (e.g. `127.0.0.1:0`, or a fixed port
// for an external app).
func NewMockServerAt(addr string) (*MockServer, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &MockServer{}
	s.server = &httptest.Server{Listener: l, Config: &http.Server{Handler: s}}
	s.server.Start()
	return s, nil
}

// URI is the server's base URL, e.g. `http://127.0.0.1:12345`.
func (s *MockServer) URI() string { return s.server.URL }

// Close shuts the server down.
func (s *MockServer) Close() { s.server.Close() }

// Mount adds a route.
func (s *MockServer) Mount(r Route) {
	if r.Priority == 0 {
		r.Priority = DefaultPriority
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = append(s.routes, &mountedRoute{Route: r})
}

func (s *MockServer) match(r *http.Request) *mountedRoute {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := r.URL.EscapedPath()
	var best *mountedRoute
	for _, m := range s.routes {
		if m.Method != "" && m.Method != r.Method {
			continue
		}
		if m.Path != "" && m.Path != path {
			continue
		}
		if m.PathRegex != nil && !m.PathRegex.MatchString(path) {
			continue
		}
		if m.Times > 0 && m.served >= m.Times {
			continue
		}
		if best == nil || m.Priority < best.Priority {
			best = m
		}
	}
	if best != nil {
		best.served++
	}
	return best
}

// ServeHTTP implements [http.Handler].
func (s *MockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m := s.match(r)
	if m == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	resp := m.Respond(r)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

func rateLimitHeaders(r Response, penalised bool) Response {
	if r.Header == nil {
		r.Header = http.Header{}
	}
	r.Header.Set("X-Rate-Limit-Policy", "trade-search-request-limit")
	r.Header.Set("X-Rate-Limit-Rules", "Ip")
	r.Header.Set("X-Rate-Limit-Ip", "5:10:60,15:60:300")
	if penalised {
		r.Header.Set("X-Rate-Limit-Ip-State", "6:10:60,6:60:0")
	} else {
		r.Header.Set("X-Rate-Limit-Ip-State", "1:10:0,1:60:0")
	}
	return r
}

func okJSON(body any) Response {
	return rateLimitHeaders(JSONResponse(http.StatusOK, body), false)
}

func fixed(r Response) func(*http.Request) Response {
	return func(*http.Request) Response { return r }
}

// MountTradeFixtures mounts static leagues, search and fetch fixtures (see [Fixture] for the
// lookup order).
func MountTradeFixtures(s *MockServer) {
	s.Mount(Route{Method: http.MethodGet, Path: LeaguesPath, Respond: fixed(okJSON(Fixture("leagues")))})
	s.Mount(Route{Method: http.MethodPost, PathRegex: searchPath, Respond: fixed(okJSON(Fixture("search")))})
	s.Mount(Route{Method: http.MethodGet, PathRegex: fetchPath, Respond: fixed(okJSON(Fixture("fetch")))})
}

// ---------------------------------------------------------------------------------------------
// TradeMock
// ---------------------------------------------------------------------------------------------

// MockListing is one listing served by [TradeMock].
type MockListing struct {
	Amount   float64
	Currency string
	Account  string
	Online   bool
}

// Endpoint is the endpoint a 429 is injected on.
type Endpoint int

const (
	EndpointLeagues Endpoint = iota + 1
	EndpointSearch
	EndpointFetch
)

type injection struct {
	endpoint          Endpoint
	times             int
	retryAfterSeconds uint64
}

// TradeMock is a programmable trade2 API for desktop / e2e tests.
//
//	server := testkit.NewMockServer()
//	testkit.NewTradeMock().Price(2, "divine").Price(3, "divine").RateLimited(testkit.EndpointSearch, 1, 2).Mount(server)
//	// point the trade client's base URL (or RKL_TRADE_BASE_URL) at server.URI()
type TradeMock struct {
	leagues    []string
	listings   []MockListing
	total      *uint64
	searchID   string
	injections []injection
}

// NewTradeMock has leagues Standard + Hardcore and no listings.
func NewTradeMock() *TradeMock {
	return &TradeMock{leagues: []string{"Standard", "Hardcore"}, searchID: "rklMock"}
}

// Leagues replaces the league list.
func (m *TradeMock) Leagues(leagues ...string) *TradeMock {
	m.leagues = append([]string(nil), leagues...)
	return m
}

// Price adds an online listing priced `amount currency`.
func (m *TradeMock) Price(amount float64, currency string) *TradeMock {
	n := len(m.listings) + 1
	m.listings = append(m.listings, MockListing{
		Amount:   amount,
		Currency: currency,
		Account:  fmt.Sprintf("seller#%04d", n),
		Online:   true,
	})
	return m
}

// MockPrice is an amount and currency for [TradeMock.Prices].
type MockPrice struct {
	Amount   float64
	Currency string
}

// Prices adds several listings.
func (m *TradeMock) Prices(prices ...MockPrice) *TradeMock {
	for _, p := range prices {
		m.Price(p.Amount, p.Currency)
	}
	return m
}

// Listing adds a fully specified listing.
func (m *TradeMock) Listing(l MockListing) *TradeMock {
	m.listings = append(m.listings, l)
	return m
}

// Total overrides the total reported by search (defaults to the listing count).
func (m *TradeMock) Total(total uint64) *TradeMock {
	m.total = &total
	return m
}

// RateLimited answers the first times calls to endpoint with 429 + Retry-After.
func (m *TradeMock) RateLimited(endpoint Endpoint, times int, retryAfterSeconds uint64) *TradeMock {
	m.injections = append(m.injections, injection{endpoint, times, retryAfterSeconds})
	return m
}

// ListingID is the id of the i-th listing (64 hex chars like the real API).
func ListingID(i int) string { return fmt.Sprintf("%064x", i+1) }

// LeaguesJSON is the leagues response.
func (m *TradeMock) LeaguesJSON() map[string]any {
	result := make([]any, 0, len(m.leagues))
	for _, l := range m.leagues {
		result = append(result, map[string]any{"id": l, "realm": "poe2", "text": l})
	}
	return map[string]any{"result": result}
}

// SearchJSON is the search response.
func (m *TradeMock) SearchJSON() map[string]any {
	ids := make([]string, len(m.listings))
	for i := range m.listings {
		ids[i] = ListingID(i)
	}
	total := uint64(len(m.listings))
	if m.total != nil {
		total = *m.total
	}
	return map[string]any{"id": m.searchID, "complexity": 4, "result": ids, "total": total}
}

// FetchJSON is the fetch response for the comma-separated ids (unknown ids yield null, like the
// API).
func (m *TradeMock) FetchJSON(ids string) map[string]any {
	result := []any{}
	for id := range strings.SplitSeq(ids, ",") {
		if id == "" {
			continue
		}
		idx := -1
		for i := range m.listings {
			if ListingID(i) == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			result = append(result, nil)
			continue
		}
		l := m.listings[idx]
		var online any
		if l.Online {
			var league any
			if len(m.leagues) > 0 {
				league = m.leagues[0]
			}
			online = map[string]any{"league": league}
		}
		result = append(result, map[string]any{
			"id": id,
			"listing": map[string]any{
				"method":  "psapi",
				"indexed": "2026-11-01T10:00:00Z",
				"account": map[string]any{
					"name":              l.Account,
					"online":            online,
					"lastCharacterName": "MockCharacter",
				},
				"price": map[string]any{"type": "~price", "amount": l.Amount, "currency": l.Currency},
			},
			"item": map[string]any{"name": "", "typeLine": "Mock Item", "baseType": "Mock Item", "frameType": 0},
		})
	}
	return map[string]any{"result": result}
}

func endpointRoute(e Endpoint) Route {
	switch e {
	case EndpointLeagues:
		return Route{Method: http.MethodGet, Path: LeaguesPath}
	case EndpointSearch:
		return Route{Method: http.MethodPost, PathRegex: searchPath}
	default:
		return Route{Method: http.MethodGet, PathRegex: fetchPath}
	}
}

// Mount mounts all routes on s. The mock's state is copied, so later builder calls do not
// change what is served.
func (m *TradeMock) Mount(s *MockServer) {
	for _, inj := range m.injections {
		r := endpointRoute(inj.endpoint)
		resp := rateLimitHeaders(JSONResponse(http.StatusTooManyRequests, map[string]any{
			"error": map[string]any{"code": 3, "message": "Rate limit exceeded"},
		}), true)
		resp.Header.Set("Retry-After", strconv.FormatUint(inj.retryAfterSeconds, 10))
		r.Respond = fixed(resp)
		r.Times = inj.times
		r.Priority = 1
		if inj.times <= 0 {
			// wiremock's up_to_n_times(0) never matches.
			continue
		}
		s.Mount(r)
	}
	snapshot := *m
	snapshot.leagues = append([]string(nil), m.leagues...)
	snapshot.listings = append([]MockListing(nil), m.listings...)
	me := &snapshot
	s.Mount(Route{Method: http.MethodGet, Path: LeaguesPath, Respond: fixed(okJSON(me.LeaguesJSON()))})
	s.Mount(Route{Method: http.MethodPost, PathRegex: searchPath, Respond: fixed(okJSON(me.SearchJSON()))})
	s.Mount(Route{Method: http.MethodGet, PathRegex: fetchPath, Respond: func(r *http.Request) Response {
		ids := strings.TrimPrefix(r.URL.EscapedPath(), FetchPrefix)
		return okJSON(me.FetchJSON(ids))
	}})
}

// Serve starts a server on addr (e.g. `127.0.0.1:0` or a fixed port for an external app) and
// mounts the mock on it.
func (m *TradeMock) Serve(addr string) (*MockServer, error) {
	s, err := NewMockServerAt(addr)
	if err != nil {
		return nil, err
	}
	m.Mount(s)
	return s, nil
}
