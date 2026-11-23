package app

import (
	"net/url"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/trade"
)

// Version is the app version, linked in from the VERSION file by the Taskfile builds and the
// packages (-ldflags -X); a plain `go build` reports "dev".
var Version = "dev"

// Production endpoints of the aggregated price sources.
var (
	ninjaBaseURL = url.URL{Scheme: "https", Host: "poe.ninja"}
	scoutBaseURL = url.URL{Scheme: "https", Host: "poe2scout.com"}
)

// baseURL is the endpoint override in key when there is one, else def.
func baseURL(key string, def url.URL) *url.URL {
	if u, ok := envOverride(key); ok {
		return u
	}
	return &def
}

// priceSources are the network price sources built from settings.
type priceSources struct {
	trade *trade.Client
	ninja *trade.NinjaSource
	scout *trade.ScoutSource
}

// userAgent is the trade User-Agent including the optional contact from settings.
func userAgent(settings *config.Settings) string {
	base := trade.DefaultUserAgent(Version)
	if c := settings.Trade.Contact; c != nil && *c != "" {
		return base + " " + *c
	}
	return base
}

func tradeConfig(s *config.Settings) trade.Config {
	cfg := trade.DefaultConfig(Version)
	if u, ok := envOverride("DIVVY_TRADE_BASE_URL"); ok {
		cfg.BaseURL = u
	}
	cfg.UserAgent = userAgent(s)
	if s.Trade.POESESSID != nil {
		cfg.POESESSID = *s.Trade.POESESSID
	}
	cfg.FetchCount = int(min(max(s.Trade.ListingCount, 10), 100))
	return cfg
}

// aggregateSources builds the poe.ninja and poe2scout sources.
func aggregateSources(ua string) (*trade.NinjaSource, *trade.ScoutSource, error) {
	ninja, err := trade.NewNinjaSource(trade.SourceOptions{
		BaseURL: baseURL("DIVVY_NINJA_BASE_URL", ninjaBaseURL), UserAgent: ua, CacheTTL: trade.DefaultSourceCacheTTL,
	})
	if err != nil {
		return nil, nil, err
	}
	scout, err := trade.NewScoutSource(trade.SourceOptions{
		BaseURL: baseURL("DIVVY_SCOUT_BASE_URL", scoutBaseURL), UserAgent: ua, CacheTTL: trade.DefaultSourceCacheTTL,
	})
	if err != nil {
		return nil, nil, err
	}
	return ninja, scout, nil
}

// newPriceSources builds every price source for s.
func newPriceSources(s *config.Settings) (*priceSources, error) {
	cfg := tradeConfig(s)
	client, err := trade.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	ninja, scout, err := aggregateSources(cfg.UserAgent)
	if err != nil {
		return nil, err
	}
	return &priceSources{trade: client, ninja: ninja, scout: scout}, nil
}

// updated returns the sources for new settings, reusing every client whose inputs are
// unchanged so a save keeps the trade rate-limiter state and the price caches. It returns src
// itself when nothing needs rebuilding.
func (src *priceSources) updated(s *config.Settings) (*priceSources, error) {
	cfg := tradeConfig(s)
	old := src.trade.Config()
	uaChanged := old.UserAgent != cfg.UserAgent
	if !uaChanged && old.BaseURL.String() == cfg.BaseURL.String() && old.POESESSID == cfg.POESESSID {
		return src, nil
	}
	client, err := trade.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	next := &priceSources{trade: client, ninja: src.ninja, scout: src.scout}
	if uaChanged {
		if next.ninja, next.scout, err = aggregateSources(cfg.UserAgent); err != nil {
			return nil, err
		}
	}
	return next, nil
}

// pick returns the configured source if it supports the item, else the official trade API.
func (src *priceSources) pick(kind core.PriceSourceKind, item *core.ParsedItem) trade.PriceSource {
	var preferred trade.PriceSource = src.trade
	switch kind {
	case core.SourcePoeNinja:
		preferred = src.ninja
	case core.SourcePoe2Scout:
		preferred = src.scout
	case core.SourceTrade:
	}
	if preferred.Supports(item) {
		return preferred
	}
	return src.trade
}
