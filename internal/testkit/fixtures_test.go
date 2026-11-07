package testkit

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	require.NoError(t, json.Unmarshal(b, &v))
	return v
}

// call sends a request and returns status, decoded JSON body (nil if not JSON) and headers.
func call(t *testing.T, method, url string) (int, map[string]any, http.Header) {
	t.Helper()
	var body *strings.Reader
	if method == http.MethodPost {
		body = strings.NewReader(`{"query":{}}`)
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, body)
	require.NoError(t, err)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var v map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v, resp.Header
}

func get(t *testing.T, url string) (int, map[string]any, http.Header) {
	return call(t, http.MethodGet, url)
}

func post(t *testing.T, url string) (int, map[string]any, http.Header) {
	return call(t, http.MethodPost, url)
}

func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func newServer(t *testing.T) *MockServer {
	s := NewMockServer()
	t.Cleanup(s.Close)
	return s
}

func TestTestkitSamplesLoad(t *testing.T) {
	assert.Len(t, at(decode(t, TestkitFixture("leagues")), "result"), 4)
	assert.InDelta(t, 3, at(decode(t, TestkitFixture("search.json")), "total"), 0)
	assert.InDelta(t, 2, at(decode(t, TestkitFixture("fetch")), "result", 1, "listing", "price", "amount"), 0)
	// falls back to testkit samples (or the recorded set) by name
	assert.IsType(t, []any{}, at(decode(t, Fixture("leagues")), "result"))
}

func TestMissingFixtureReportsPaths(t *testing.T) {
	_, err := TryFixture("definitely-not-a-fixture")
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "definitely-not-a-fixture")
	assert.Contains(t, msg, filepath.Join("trade", "testdata", "fixtures"))
	assert.Contains(t, msg, filepath.Join("testkit", "testdata", "fixtures"))
}

func TestFixturePanicsReadably(t *testing.T) {
	defer func() { assert.Contains(t, recover(), "not found") }()
	Fixture("nope")
}

func TestInvalidJSONAndIOErrors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "dir.json"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{nope"), 0o644))
	_, err := TryFixtureIn([]string{dir}, "bad")
	var fe *FixtureError
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, FixtureJSON, fe.Kind)
	assert.Contains(t, err.Error(), "not valid JSON")
	_, err = readJSON(filepath.Join(dir, "dir.json"))
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, FixtureIO, fe.Kind)
	assert.Contains(t, err.Error(), "reading fixture")
}

func TestFixtureHeaders(t *testing.T) {
	// Not in the Rust suite: the recorded header sets the trade tests attach to responses.
	h, err := FixtureHeaders("search.429")
	require.NoError(t, err)
	assert.Equal(t, "Ip", h.Get("X-Rate-Limit-Rules"))
	assert.Equal(t, "60", h.Get("Retry-After"))
	_, err = FixtureHeaders("nope")
	assert.Error(t, err)
}

func TestMountsStaticFixtures(t *testing.T) {
	server := newServer(t)
	MountTradeFixtures(server)
	s, v, h := get(t, server.URI()+LeaguesPath)
	assert.Equal(t, 200, s)
	assert.IsType(t, []any{}, v["result"])
	assert.Equal(t, "Ip", h.Get("x-rate-limit-rules"))
	s, v, _ = post(t, server.URI()+SearchPrefix+"Standard")
	assert.Equal(t, 200, s)
	assert.IsType(t, []any{}, v["result"])
	s, v, _ = get(t, server.URI()+FetchPrefix+"abc,def?query=q")
	assert.Equal(t, 200, s)
	assert.IsType(t, []any{}, v["result"])
	// Unmatched requests are 404, as with wiremock.
	s, _, _ = get(t, server.URI()+SearchPrefix+"Standard")
	assert.Equal(t, 404, s)
}

func TestTradeMockServesPricesByID(t *testing.T) {
	server := newServer(t)
	NewTradeMock().
		Leagues("Standard").
		Prices(MockPrice{1.0, "divine"}, MockPrice{2.5, "exalted"}).
		Listing(MockListing{Amount: 9.0, Currency: "chaos", Account: "x", Online: false}).
		Total(120).
		Mount(server)
	_, v, _ := get(t, server.URI()+LeaguesPath)
	assert.Equal(t, "Standard", at(v, "result", 0, "id"))
	_, search, _ := post(t, server.URI()+SearchPrefix+"Standard")
	assert.InDelta(t, 120, search["total"], 0)
	var ids []string
	for _, id := range search["result"].([]any) {
		ids = append(ids, id.(string))
	}
	require.Len(t, ids, 3)
	assert.Len(t, ids[0], 64)
	url := server.URI() + FetchPrefix + ids[1] + "," + ids[2] + ",unknown?query=" + search["id"].(string)
	s, f, _ := get(t, url)
	assert.Equal(t, 200, s)
	assert.InDelta(t, 2.5, at(f, "result", 0, "listing", "price", "amount"), 0)
	assert.Equal(t, "exalted", at(f, "result", 0, "listing", "price", "currency"))
	account := at(f, "result", 1, "listing", "account").(map[string]any)
	assert.Contains(t, account, "online")
	assert.Nil(t, account["online"])
	assert.Len(t, f["result"], 3)
	assert.Nil(t, at(f, "result", 2))
}

func TestTradeMockInjects429ThenRecovers(t *testing.T) {
	server := newServer(t)
	NewTradeMock().
		Price(1.0, "divine").
		RateLimited(EndpointSearch, 2, 7).
		RateLimited(EndpointLeagues, 1, 1).
		RateLimited(EndpointFetch, 1, 3).
		Mount(server)
	search := server.URI() + SearchPrefix + "Standard"
	for range 2 {
		s, v, h := post(t, search)
		assert.Equal(t, 429, s)
		assert.Equal(t, "7", h.Get("retry-after"))
		assert.InDelta(t, 3, at(v, "error", "code"), 0)
	}
	s, _, _ := post(t, search)
	assert.Equal(t, 200, s)
	leagues := server.URI() + LeaguesPath
	s, _, _ = get(t, leagues)
	assert.Equal(t, 429, s)
	s, _, _ = get(t, leagues)
	assert.Equal(t, 200, s)
	fetch := server.URI() + FetchPrefix + ListingID(0)
	s, _, _ = get(t, fetch)
	assert.Equal(t, 429, s)
	s, v, _ := get(t, fetch)
	assert.Equal(t, 200, s)
	assert.InDelta(t, 1.0, at(v, "result", 0, "listing", "price", "amount"), 0)
}

func TestServeBindsAddress(t *testing.T) {
	server, err := NewTradeMock().Serve("127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(server.Close)
	s, v, _ := get(t, server.URI()+LeaguesPath)
	assert.Equal(t, 200, s)
	assert.Len(t, v["result"], 2)
	_, err = NewTradeMock().Serve("256.0.0.1:1")
	assert.Error(t, err)
}
