package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPicksPoe2League(t *testing.T) {
	check := func(raw, want string, wantOK bool) {
		t.Helper()
		got, ok := pickLeague(decode(t, raw))
		if got != want || ok != wantOK {
			t.Errorf("pickLeague(%s) = %q, %v", raw, got, ok)
		}
	}
	check(`{"result":[{"id":"Standard","realm":"pc"},{"id":"Dawn of the Hunt","realm":"poe2"}]}`, "Dawn of the Hunt", true)
	check(`{"result":[{"id":"Standard"}]}`, "Standard", true)
	check(`{}`, "", false)
	check(`[]`, "", false)
}

func TestBuildsFetchPath(t *testing.T) {
	search := decode(t, `{"id":"Q1","result":["a","b","c"],"total":3}`)
	got, err := fetchPath(search, 2)
	eq(t, got, "/api/trade2/fetch/a,b?query=Q1")
	eq(t, err, nil)
	got, _ = fetchPath(search, 50)
	eq(t, got, "/api/trade2/fetch/a,b,c?query=Q1")
	if _, err := fetchPath(decode(t, `{"id":"Q","result":[]}`), 10); err == nil {
		t.Error("empty result accepted")
	}
	if _, err := fetchPath(decode(t, `{"result":["a"]}`), 10); err == nil {
		t.Error("missing id accepted")
	}
}

func TestEscapesAndDescribes(t *testing.T) {
	eq(t, urlEscape("Dawn of the Hunt"), "Dawn%20of%20the%20Hunt")
	eq(t, urlEscape("HC_SSF-1.0~"), "HC_SSF-1.0~")
	eq(t, urlEscape("Ü/"), "%C3%9C%2F")
	if !strings.Contains(fixturesUserAgent("1.2.3"), "github.com/BorrowedMirror/rust-king-ledger") {
		t.Error("user agent lacks the repository")
	}
	query, _ := searchBody("Leather Belt")["query"].(map[string]any)
	eq(t, query["type"], any("Leather Belt"))
}

func TestPrettyJSONLikeSerde(t *testing.T) {
	var v any
	dec := json.NewDecoder(strings.NewReader(`{"b":[1,2.50,{}],"a":"<&>","c":[],"d":1e2}`))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	got, err := prettyJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, string(got), "{\n  \"a\": \"<&>\",\n  \"b\": [\n    1,\n    2.5,\n    {}\n  ],\n  \"c\": [],\n  \"d\": 100.0\n}\n")
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
		if ua := r.Header.Get("User-Agent"); ua != "test-agent" {
			t.Errorf("User-Agent = %q", ua)
		}
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/trade2/data/leagues":
			_, _ = io.WriteString(w, `{"result":[{"id":"Standard","realm":"pc"},{"id":"Rise of the Abyssal","realm":"poe2"}]}`)
		case "/api/trade2/data/stats", "/api/trade2/data/items", "/api/trade2/data/static":
			_, _ = io.WriteString(w, `{"result":[{"id":"`+filepath.Base(r.URL.Path)+`","entries":[]}]}`)
		case "/api/trade2/search/poe2/Rise of the Abyssal":
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
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
	if err != nil {
		t.Fatal(err)
	}
	eq(t, league, "Rise of the Abyssal")
	eq(t, requests, []string{
		"GET /api/trade2/data/leagues",
		"GET /api/trade2/data/stats",
		"GET /api/trade2/data/items",
		"GET /api/trade2/data/static",
		"POST /api/trade2/search/poe2/Rise%20of%20the%20Abyssal",
		"GET /api/trade2/fetch/a,b?query=Q1",
	})
	eq(t, search, `{"query":{"stats":[{"filters":[],"type":"and"}],"status":{"option":"online"},"type":"Leather Belt"},"sort":{"price":"asc"}}`)
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
		if err != nil {
			t.Error(err)
			continue
		}
		if body != "" && string(b) != body {
			t.Errorf("%s:\n%s\nwant\n%s", name, b, body)
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
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "slow down") {
		t.Fatalf("err = %v", err)
	}
}
