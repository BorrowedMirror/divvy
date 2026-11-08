package main

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(s), &v))
	return v
}

func TestPicksPoe2League(t *testing.T) {
	check := func(raw, want string, wantOK bool) {
		t.Helper()
		got, ok := pickLeague(decode(t, raw))
		assert.Equal(t, want, got, raw)
		assert.Equal(t, wantOK, ok, raw)
	}
	check(`{"result":[{"id":"Standard","realm":"pc"},{"id":"Dawn of the Hunt","realm":"poe2"}]}`, "Dawn of the Hunt", true)
	check(`{"result":[{"id":"Standard"}]}`, "Standard", true)
	check(`{}`, "", false)
	check(`[]`, "", false)
}

func TestBuildsFetchPath(t *testing.T) {
	search := decode(t, `{"id":"Q1","result":["a","b","c"],"total":3}`)
	got, err := fetchPath(search, 2)
	require.NoError(t, err)
	assert.Equal(t, "/api/trade2/fetch/a,b?query=Q1", got)
	got, _ = fetchPath(search, 50)
	assert.Equal(t, "/api/trade2/fetch/a,b,c?query=Q1", got)
	_, err = fetchPath(decode(t, `{"id":"Q","result":[]}`), 10)
	require.Error(t, err, "empty result accepted")
	_, err = fetchPath(decode(t, `{"result":["a"]}`), 10)
	require.Error(t, err, "missing id accepted")
}

func TestDescribes(t *testing.T) {
	assert.Contains(t, fixturesUserAgent("1.2.3"), "github.com/BorrowedMirror/rust-king-ledger")
	query, _ := searchBody("Leather Belt")["query"].(map[string]any)
	assert.Equal(t, any("Leather Belt"), query["type"])
}

func TestPrettyJSONSortsAndIndents(t *testing.T) {
	got, err := prettyJSON(decode(t, `{"b":[1,2.50,{}],"a":"<&>","c":[],"d":1e2}`))
	require.NoError(t, err)
	//nolint:testifylint // the formatting is under test, not only the value
	assert.Equal(t, "{\n  \"a\": \"<&>\",\n  \"b\": [\n    1,\n    2.5,\n    {}\n  ],\n  \"c\": [],\n  \"d\": 100\n}\n", string(got))
}

// TestRecordWritesEveryFixture runs a whole recording against a fake trade2 API.
func TestRecordWritesEveryFixture(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
		search   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "test-agent", r.Header.Get("User-Agent"))
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/trade2/data/leagues":
			_, _ = io.WriteString(w, `{"result":[{"id":"Standard","realm":"pc"},{"id":"Rise of the Abyssal","realm":"poe2"}]}`)
		case "/api/trade2/data/stats", "/api/trade2/data/items", "/api/trade2/data/static":
			_, _ = io.WriteString(w, `{"result":[{"id":"`+filepath.Base(r.URL.Path)+`","entries":[]}]}`)
		case "/api/trade2/search/poe2/Rise of the Abyssal":
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			b, _ := io.ReadAll(r.Body)
			search = string(b)
			_, _ = io.WriteString(w, `{"id":"Q1","result":["a","b","c"],"total":3}`)
		case "/api/trade2/fetch/a,b":
			_, _ = io.WriteString(w, `{"result":[{"id":"a","price":1.50},{"id":"b"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "fixtures")
	opts := recordOptions{base: srv.URL, out: out, baseType: "Leather Belt", fetchMax: 2, delay: time.Millisecond, userAgent: "test-agent"}
	league, err := record(t.Context(), opts, srv.Client())
	require.NoError(t, err)
	assert.Equal(t, "Rise of the Abyssal", league)
	assert.Equal(t, []string{
		"GET /api/trade2/data/leagues",
		"GET /api/trade2/data/stats",
		"GET /api/trade2/data/items",
		"GET /api/trade2/data/static",
		"POST /api/trade2/search/poe2/Rise%20of%20the%20Abyssal",
		"GET /api/trade2/fetch/a,b?query=Q1",
	}, requests)
	assert.JSONEq(t, `{"query":{"stats":[{"filters":[],"type":"and"}],"status":{"option":"online"},"type":"Leather Belt"},"sort":{"price":"asc"}}`, search)
	want := map[string]string{
		"leagues.json":     "{\n  \"result\": [\n    {\n      \"id\": \"Standard\",\n      \"realm\": \"pc\"\n    },\n    {\n      \"id\": \"Rise of the Abyssal\",\n      \"realm\": \"poe2\"\n    }\n  ]\n}\n",
		"data_items.json":  "{\n  \"result\": [\n    {\n      \"entries\": [],\n      \"id\": \"items\"\n    }\n  ]\n}\n",
		"search.json":      "{\n  \"id\": \"Q1\",\n  \"result\": [\n    \"a\",\n    \"b\",\n    \"c\"\n  ],\n  \"total\": 3\n}\n",
		"fetch.json":       "{\n  \"result\": [\n    {\n      \"id\": \"a\",\n      \"price\": 1.5\n    },\n    {\n      \"id\": \"b\"\n    }\n  ]\n}\n",
		"data_stats.json":  "",
		"data_static.json": "",
	}
	for name, body := range want {
		b, err := os.ReadFile(filepath.Join(out, name))
		require.NoError(t, err)
		if body != "" {
			assert.Equal(t, body, string(b), name)
		}
	}
}

func TestRecordStopsOnHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	opts := recordOptions{base: srv.URL, out: t.TempDir(), userAgent: "x"}
	_, err := record(t.Context(), opts, srv.Client())
	require.ErrorContains(t, err, "429")
	assert.ErrorContains(t, err, "slow down")
}
