package main

// `fixtures`: re-record the trade2 fixtures in crates/trade/tests/fixtures/ from the live
// API, slowly (default 2 s between requests, well under the published rate limits). Used by
// .github/workflows/fixtures-refresh.yml, whose output is reviewed as a pull request.
//
// Recorded files: leagues.json, data_{stats,items,static}.json, search.json (one search for
// a common unique base) and fetch.json (the first -fetch-count ids of that search).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

const tradeBase = "https://www.pathofexile.com"

// recordOptions configures one recording run.
type recordOptions struct {
	base      string // API origin, e.g. tradeBase
	out       string
	league    string // empty: the first poe2 league from the API
	baseType  string
	fetchMax  int
	delay     time.Duration
	userAgent string
}

func fixturesUserAgent(version string) string {
	return "rkl-fixtures/" + version + " (fixture recorder; +https://github.com/BorrowedMirror/rust-king-ledger)"
}

// searchBody is the trade2 search body recorded into search.json.
func searchBody(baseType string) map[string]any {
	return map[string]any{
		"query": map[string]any{
			"status": map[string]any{"option": "online"},
			"type":   baseType,
			"stats":  []any{map[string]any{"type": "and", "filters": []any{}}},
		},
		"sort": map[string]any{"price": "asc"},
	}
}

// pickLeague returns the id of the first league whose realm is poe2 or unset.
func pickLeague(leagues any) (string, bool) {
	obj, _ := leagues.(map[string]any)
	list, ok := obj["result"].([]any)
	if !ok {
		return "", false
	}
	for _, l := range list {
		league, _ := l.(map[string]any)
		if realm, ok := league["realm"].(string); ok && realm != "poe2" {
			continue
		}
		id, ok := league["id"].(string)
		return id, ok
	}
	return "", false
}

// fetchPath is the fetch request for the first count (at most 10) results of search.
func fetchPath(search any, count int) (string, error) {
	obj, _ := search.(map[string]any)
	id, ok := obj["id"].(string)
	if !ok {
		return "", errors.New("search response without `id`")
	}
	results, ok := obj["result"].([]any)
	if !ok {
		return "", errors.New("search response without `result`")
	}
	var ids []string
	for _, r := range results {
		if len(ids) == min(count, 10) {
			break
		}
		if s, ok := r.(string); ok {
			ids = append(ids, s)
		}
	}
	if len(ids) == 0 {
		return "", errors.New("search returned no results; pick another -base-type")
	}
	return "/api/trade2/fetch/" + strings.Join(ids, ",") + "?query=" + id, nil
}

// urlEscape percent-encodes a league name for use as a path segment.
func urlEscape(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// prettyJSON formats v like serde_json::to_string_pretty (sorted keys, two-space indent)
// plus a newline.
func prettyJSON(v any) ([]byte, error) {
	compact, err := core.MarshalSerde(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// recorder sends the requests of one run, pausing between them.
type recorder struct {
	opts   recordOptions
	client *http.Client
	sent   int
}

func (r *recorder) call(ctx context.Context, path string, body any) (any, error) {
	if r.sent > 0 {
		time.Sleep(r.opts.delay)
	}
	r.sent++
	url := r.opts.base + path
	logf("-> %s", url)
	method, reader := http.MethodGet, io.Reader(http.NoBody)
	if body != nil {
		b, err := core.MarshalSerde(body)
		if err != nil {
			return nil, err
		}
		method, reader = http.MethodPost, bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.opts.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: HTTP %s: %s", url, resp.Status, raw)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return v, nil
}

func (r *recorder) write(name string, v any) error {
	b, err := prettyJSON(v)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.opts.out, name), b, 0o644)
}

// record re-records every fixture into opts.out, returning the league searched.
func record(ctx context.Context, opts recordOptions, client *http.Client) (string, error) {
	if err := os.MkdirAll(opts.out, 0o755); err != nil {
		return "", err
	}
	r := &recorder{opts: opts, client: client}
	leagues, err := r.call(ctx, "/api/trade2/data/leagues", nil)
	if err != nil {
		return "", err
	}
	if err := r.write("leagues.json", leagues); err != nil {
		return "", err
	}
	for _, which := range []string{"stats", "items", "static"} {
		v, err := r.call(ctx, "/api/trade2/data/"+which, nil)
		if err != nil {
			return "", err
		}
		if err := r.write("data_"+which+".json", v); err != nil {
			return "", err
		}
	}
	league := opts.league
	if league == "" {
		var ok bool
		if league, ok = pickLeague(leagues); !ok {
			return "", errors.New("no league in leagues response")
		}
	}
	search, err := r.call(ctx, "/api/trade2/search/poe2/"+urlEscape(league), searchBody(opts.baseType))
	if err != nil {
		return "", err
	}
	if err := r.write("search.json", search); err != nil {
		return "", err
	}
	path, err := fetchPath(search, opts.fetchMax)
	if err != nil {
		return "", err
	}
	fetched, err := r.call(ctx, path, nil)
	if err != nil {
		return "", err
	}
	return league, r.write("fetch.json", fetched)
}

func runFixtures(args []string) error {
	fs := flag.NewFlagSet("fixtures", flag.ContinueOnError)
	league := fs.String("league", "", "league to search in (default: the first poe2 league returned by the API)")
	out := fs.String("out", "", "output directory (default: crates/trade/tests/fixtures)")
	delayMS := fs.Int("delay-ms", 2000, "delay between requests in milliseconds")
	fetchCount := fs.Int("fetch-count", 10, "number of listings to fetch (trade2 caps this at 10)")
	baseType := fs.String("base-type", "Leather Belt", "base type of the unique to search for")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("fixtures: unexpected arguments %q", fs.Args())
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(root, "crates", "trade", "tests", "fixtures")
	}
	version, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return err
	}
	opts := recordOptions{
		base:      tradeBase,
		out:       *out,
		league:    *league,
		baseType:  *baseType,
		fetchMax:  *fetchCount,
		delay:     time.Duration(*delayMS) * time.Millisecond,
		userAgent: fixturesUserAgent(strings.TrimSpace(string(version))),
	}
	got, err := record(context.Background(), opts, &http.Client{Timeout: 60 * time.Second})
	if err != nil {
		return err
	}
	logf("recorded fixtures for league `%s` into %s", got, *out)
	return nil
}
