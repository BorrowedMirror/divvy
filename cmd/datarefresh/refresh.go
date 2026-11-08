package main

// `refresh [-offline]`: download upstream game data, normalise it into internal/core
// catalog records and write internal/data/assets/.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
)

// userAgent is descriptive, as GGG asks of API consumers.
const userAgent = "rkl-data-refresh/0.1 (+https://github.com/BorrowedMirror/rust-king-ledger)"

// politeDelay is the pause between requests, to stay well under rate limits.
const politeDelay = 1500 * time.Millisecond

const (
	trade2 = "https://www.pathofexile.com/api/trade2/data"
	repoe  = "https://repoe-fork.github.io/poe2"
)

// source is one upstream download, cached as target/data-cache/<cache>.
type source struct {
	cache, base, path string
}

func (s source) url() string { return s.base + "/" + s.path }

var (
	tradeStats   = source{"trade2-stats.json", trade2, "stats"}
	tradeItems   = source{"trade2-items.json", trade2, "items"}
	tradeStatic  = source{"trade2-static.json", trade2, "static"}
	repoeBases   = source{"repoe-base_items.min.json", repoe, "base_items.min.json"}
	repoeClasses = source{"repoe-item_classes.min.json", repoe, "item_classes.min.json"}
	repoeIndex   = source{"repoe-index.html", repoe, ""}

	allSources = []source{tradeStats, tradeItems, tradeStatic, repoeBases, repoeClasses, repoeIndex}
)

// fetcher downloads one URL.
type fetcher func(ctx context.Context, url string) ([]byte, error)

// httpFetch GETs url with the refresh User-Agent, failing on a non-2xx status.
func httpFetch(client *http.Client) fetcher {
	return func(ctx context.Context, url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("%s: HTTP %s", url, resp.Status)
		}
		return body, nil
	}
}

// fetchAll downloads every source into cacheDir (unless fetch is nil, i.e. offline) and
// returns their contents keyed by cache name.
func fetchAll(ctx context.Context, cacheDir string, fetch fetcher, delay time.Duration) (map[string][]byte, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(allSources))
	for i, src := range allSources {
		path := filepath.Join(cacheDir, src.cache)
		if fetch != nil {
			if i > 0 {
				if err := sleep(ctx, delay); err != nil {
					return nil, err
				}
			}
			slog.Info("fetch", "url", src.url())
			body, err := fetch(ctx, src.url())
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, body, 0o644); err != nil {
				return nil, err
			}
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("missing cached download %s (run without -offline first): %w", path, err)
		}
		out[src.cache] = body
	}
	return out, nil
}

// buildBundle normalises the cached downloads into a bundle (without translations).
func buildBundle(raw map[string][]byte) (*data.Bundle, error) {
	bases, err := decodeRepoeBases(raw[repoeBases.cache])
	if err != nil {
		return nil, fmt.Errorf("parsing RePoE base_items: %w", err)
	}
	var classes map[string]repoeItemClass
	if err := json.Unmarshal(raw[repoeClasses.cache], &classes); err != nil {
		return nil, fmt.Errorf("parsing RePoE item_classes: %w", err)
	}
	var b data.Bundle
	if b.Stats, err = normaliseStats(raw[tradeStats.cache]); err != nil {
		return nil, err
	}
	if b.Bases, err = normaliseBases(raw[tradeItems.cache], bases, classes); err != nil {
		return nil, err
	}
	if b.Uniques, err = normaliseUniques(raw[tradeItems.cache]); err != nil {
		return nil, err
	}
	if b.Statics, err = normaliseStatics(raw[tradeStatic.cache]); err != nil {
		return nil, err
	}
	return &b, nil
}

func assetSources() data.AssetSources {
	return data.AssetSources{
		Stats:   []string{tradeStats.url()},
		Bases:   []string{tradeItems.url(), repoeBases.url(), repoeClasses.url()},
		Uniques: []string{tradeItems.url()},
		Statics: []string{tradeStatic.url()},
	}
}

func runRefresh(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("refresh", flag.ContinueOnError)
	offline := flags.Bool("offline", false, "use the cached downloads in the cache directory instead of the network")
	cacheDir := flags.String("cache", "", "download cache (default: target/data-cache)")
	assetsDir := flags.String("assets", "", "asset directory to write (default: internal/data/assets)")
	check := flags.Bool("check", true, "run the parser corpus tests against the new data afterwards")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("refresh: unexpected arguments %q", flags.Args())
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	if *cacheDir == "" {
		*cacheDir = filepath.Join(root, "target", "data-cache")
	}
	if *assetsDir == "" {
		*assetsDir = filepath.Join(root, "internal", "data", "assets")
	}
	var fetch fetcher
	if !*offline {
		fetch = httpFetch(&http.Client{Timeout: 60 * time.Second})
	}
	if err := refresh(ctx, *cacheDir, *assetsDir, fetch, time.Now()); err != nil {
		return err
	}
	if *check {
		corpusCheck(ctx, root)
	}
	return nil
}

// refresh fetches (unless fetch is nil), normalises and writes the assets, keeping the
// translations of the assets already in assetsDir.
func refresh(ctx context.Context, cacheDir, assetsDir string, fetch fetcher, now time.Time) error {
	raw, err := fetchAll(ctx, cacheDir, fetch, politeDelay)
	if err != nil {
		return err
	}
	bundle, err := buildBundle(raw)
	if err != nil {
		return err
	}
	sources := assetSources()
	// Translations come from an installed client (`l10n`), not from the network: keep them.
	if previous, err := data.LoadDir(assetsDir); err == nil {
		bundle.Languages = previous.Bundle().Languages
		if previous.Manifest.L10n != nil {
			sources.L10n = previous.Manifest.L10n.Sources
		}
	}
	generatedAt := now.UTC().Format(time.RFC3339Nano)
	manifest, err := data.WriteDir(assetsDir, bundle, &sources, generatedAt, gameVersion(string(raw[repoeIndex.cache])))
	if err != nil {
		return err
	}
	// Round-trip what was written so a broken asset never gets committed.
	if _, err := data.LoadDir(assetsDir); err != nil {
		return fmt.Errorf("re-loading freshly written assets: %w", err)
	}
	version := "unknown"
	if manifest.GameVersion != nil {
		version = *manifest.GameVersion
	}
	slog.Info("wrote assets", "dir", assetsDir, "stats", manifest.Stats.Records, "bases", manifest.Bases.Records,
		"uniques", manifest.Uniques.Records, "statics", manifest.Statics.Records, "game_version", version)
	return nil
}

// corpusCheck runs the parser/matcher tests, which embed the assets, against the new data.
// A failure is reported loudly but does not fail the refresh: review the output before
// committing regenerated assets.
func corpusCheck(ctx context.Context, root string) {
	slog.Info("running parser corpus check (go test ./internal/core/... ./internal/data/...)")
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/core/...", "./internal/data/...")
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); !ok {
			slog.Warn("corpus check skipped", "err", err)
			return
		}
		slog.Warn("corpus check FAILED; inspect the failures before committing the new assets", "err", err)
		return
	}
	slog.Info("corpus check passed")
}
