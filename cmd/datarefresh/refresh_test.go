package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
)

func TestSourceURLs(t *testing.T) {
	assert.Equal(t, "https://www.pathofexile.com/api/trade2/data/stats", tradeStats.url())
	assert.Equal(t, "https://repoe-fork.github.io/poe2/", repoeIndex.url())
	assert.Contains(t, userAgent, "rkl")
}

func TestOfflineWithoutCacheErrors(t *testing.T) {
	_, err := fetchAll(t.Context(), t.TempDir(), nil, 0)
	assert.ErrorContains(t, err, "-offline")
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
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
}

func TestBuildBundleFromCachedSamples(t *testing.T) {
	dir := t.TempDir()
	writeSamples(t, dir)
	raw, err := fetchAll(t.Context(), dir, nil, 0)
	require.NoError(t, err)
	b, err := buildBundle(raw)
	require.NoError(t, err)
	assert.Len(t, b.Stats, 1)
	assert.Equal(t, "armour.helmet", b.Bases[0].Category)
	assert.Equal(t, "Divine Orb", b.Statics[0].Text)
	assert.Empty(t, b.Uniques)
	assert.Len(t, assetSources().Bases, 3)
}

func TestHTTPFetchSendsUserAgentAndChecksStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"))
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	fetch := httpFetch(srv.Client())
	body, err := fetch(t.Context(), srv.URL+"/ok")
	require.NoError(t, err)
	assert.Equal(t, "body", string(body))
	_, err = fetch(t.Context(), srv.URL+"/missing")
	assert.ErrorContains(t, err, "404")
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
		return nil, fmt.Errorf("unexpected url %s", url)
	}
	raw, err := fetchAll(t.Context(), dir, fetch, 0)
	require.NoError(t, err)
	assert.Len(t, urls, len(allSources))
	for _, s := range allSources {
		assert.Equal(t, samples[s.cache], string(raw[s.cache]))
		b, err := os.ReadFile(filepath.Join(dir, s.cache))
		require.NoError(t, err)
		assert.Equal(t, samples[s.cache], string(b))
	}
}

func TestRefreshWritesAssetsAndKeepsTranslations(t *testing.T) {
	cache, assets := t.TempDir(), t.TempDir()
	writeSamples(t, cache)
	// Previous assets with one language: refresh keeps it and its sources.
	prev := data.Bundle{Languages: []core.Language{{Name: "German", Code: "de"}}}
	_, err := data.WriteDir(assets, &prev, &data.AssetSources{L10n: []string{"client"}}, "then", nil)
	require.NoError(t, err)
	now := time.Date(2026, 1, 2, 3, 4, 5, 600, time.UTC)
	require.NoError(t, refresh(t.Context(), cache, assets, nil, now))
	got, err := data.LoadDir(assets)
	require.NoError(t, err)
	m := got.Manifest
	assert.Equal(t, "2026-01-02T03:04:05.0000006Z", m.GeneratedAt)
	assert.Equal(t, "0.5.5.5", *m.GameVersion)
	assert.Equal(t, 1, m.Stats.Records)
	assert.Equal(t, assetSources().Bases, m.Bases.Sources)
	assert.Equal(t, []string{"client"}, m.L10n.Sources)
	assert.Equal(t, "de", got.Bundle().Languages[0].Code)
}
