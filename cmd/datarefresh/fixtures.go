package main

// `fixtures`: re-record the trade2 fixtures in internal/trade/testdata/fixtures/ from the live
// API, slowly (default 2 s between requests, well under the published rate limits). Used by
// .github/workflows/fixtures-refresh.yml, whose output is reviewed as a pull request.
//
// Recorded files: leagues.json, data_{stats,items,static}.json, search.json (one search for
// a common unique base) and fetch.json (the first -fetch-count ids of that search).

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	return "divvy-fixtures/" + version + " (fixture recorder; +https://github.com/BorrowedMirror/divvy)"
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

// prettyJSON formats v with sorted keys and a two-space indent, plus a newline, so
// re-recorded fixtures diff cleanly.
func prettyJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// recorder sends the requests of one run, pausing between them.
type recorder struct {
	opts   recordOptions
	client *http.Client
	sent   int
}

func (r *recorder) call(ctx context.Context, path string, body any) (any, error) {
	if r.sent > 0 {
		if err := sleep(ctx, r.opts.delay); err != nil {
			return nil, err
		}
	}
	r.sent++
	target := r.opts.base + path
	slog.Info("request", "url", target)
	method, reader := http.MethodGet, io.Reader(http.NoBody)
	if body != nil {
		b, err := json.Marshal(body, json.Deterministic(true))
		if err != nil {
			return nil, err
		}
		method, reader = http.MethodPost, bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
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
		return nil, fmt.Errorf("%s: HTTP %s: %s", target, resp.Status, raw)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", target, err)
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
	search, err := r.call(ctx, "/api/trade2/search/poe2/"+url.PathEscape(league), searchBody(opts.baseType))
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

func runFixtures(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("fixtures", flag.ContinueOnError)
	league := flags.String("league", "", "league to search in (default: the first poe2 league returned by the API)")
	out := flags.String("out", "", "output directory (default: internal/trade/testdata/fixtures)")
	delayMS := flags.Int("delay-ms", 2000, "delay between requests in milliseconds")
	fetchCount := flags.Int("fetch-count", 10, "number of listings to fetch (trade2 caps this at 10)")
	baseType := flags.String("base-type", "Leather Belt", "base type of the unique to search for")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("fixtures: unexpected arguments %q", flags.Args())
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(root, "internal", "trade", "testdata", "fixtures")
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
	got, err := record(ctx, opts, &http.Client{Timeout: 60 * time.Second})
	if err != nil {
		return err
	}
	slog.Info("recorded fixtures", "league", got, "dir", *out)
	return nil
}
