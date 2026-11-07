package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
)

func TestSourceURLs(t *testing.T) {
	eq(t, tradeStats.url(), "https://www.pathofexile.com/api/trade2/data/stats")
	eq(t, repoeIndex.url(), "https://repoe-fork.github.io/poe2/")
	if !strings.Contains(userAgent, "rkl") {
		t.Errorf("user agent %q", userAgent)
	}
}

func TestOfflineWithoutCacheErrors(t *testing.T) {
	_, err := fetchAll(t.Context(), t.TempDir(), nil, 0)
	if err == nil || !strings.Contains(err.Error(), "-offline") {
		t.Fatalf("err = %v", err)
	}
}

// samples are minimal cached downloads.
var samples = map[string]string{
	tradeStats.cache:   `{"result":[{"id":"explicit","entries":[{"id":"explicit.a","text":"# to Spirit"}]}]}`,
	tradeItems.cache:   `{"result":[{"id":"armour","label":"Armour","entries":[{"type":"Hunter Hood"}]}]}`,
	tradeStatic.cache:  `{"result":[{"id":"Currency","entries":[{"id":"divine","text":"Divine Orb"}]}]}`,
	repoeBases.cache:   `{"x":{"name":"Hunter Hood","item_class":"Helmet","release_state":"released"}}`,
	repoeClasses.cache: `{"Helmet":{"name":"Helmets"}}`,
	repoeIndex.cache:   "PoE2 version 0.5.5.5",
}

func writeSamples(t *testing.T, dir string) {
	t.Helper()
	for name, body := range samples {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildBundleFromCachedSamples(t *testing.T) {
	dir := t.TempDir()
	writeSamples(t, dir)
	raw, err := fetchAll(t.Context(), dir, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(b.Stats), 1)
	eq(t, b.Bases[0].Category, "armour.helmet")
	eq(t, b.Statics[0].Text, "Divine Orb")
	eq(t, len(b.Uniques), 0)
	eq(t, len(assetSources().Bases), 3)
}

func TestHTTPFetchSendsUserAgentAndChecksStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	fetch := httpFetch(srv.Client())
	body, err := fetch(t.Context(), srv.URL+"/ok")
	if err != nil || string(body) != "body" {
		t.Fatalf("fetch = %q, %v", body, err)
	}
	if _, err := fetch(t.Context(), srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchAllDownloadsEverySourceIntoTheCache(t *testing.T) {
	dir := t.TempDir()
	var urls []string
	fetch := func(_ context.Context, url string) ([]byte, error) {
		urls = append(urls, url)
		for _, s := range allSources {
			if s.url() == url {
				return []byte(samples[s.cache]), nil
			}
		}
		t.Fatalf("unexpected url %s", url)
		return nil, nil
	}
	raw, err := fetchAll(t.Context(), dir, fetch, 0)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(urls), len(allSources))
	for _, s := range allSources {
		eq(t, string(raw[s.cache]), samples[s.cache])
		if b, err := os.ReadFile(filepath.Join(dir, s.cache)); err != nil || string(b) != samples[s.cache] {
			t.Errorf("cache %s = %q, %v", s.cache, b, err)
		}
	}
}

func TestRefreshWritesAssetsAndKeepsTranslations(t *testing.T) {
	cache, assets := t.TempDir(), t.TempDir()
	writeSamples(t, cache)
	// Previous assets with one language: refresh keeps it and its sources.
	prev := data.Bundle{Languages: []core.Language{{Name: "German", Code: "de"}}}
	_, err := data.WriteDir(assets, &prev, &data.AssetSources{L10n: []string{"client"}}, "then", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 600, time.UTC)
	if err := refresh(t.Context(), cache, assets, nil, now); err != nil {
		t.Fatal(err)
	}
	got, err := data.LoadDir(assets)
	if err != nil {
		t.Fatal(err)
	}
	m := got.Manifest
	eq(t, m.GeneratedAt, "2026-01-02T03:04:05.0000006Z")
	eq(t, *m.GameVersion, "0.5.5.5")
	eq(t, m.Stats.Records, 1)
	eq(t, m.Bases.Sources, assetSources().Bases)
	eq(t, m.L10n.Sources, []string{"client"})
	eq(t, got.Bundle().Languages[0].Code, "de")
}
