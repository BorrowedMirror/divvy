package trade

// poe2scout aggregated prices (`https://api.poe2scout.com`, see docs/research/ninja-scout.md).

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"

	"github.com/maypok86/otter/v2"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

const (
	scoutSource = "poe2scout"
	scoutRealm  = "poe2"
)

type scoutLeague struct {
	Value             string   `json:"Value"`
	DivinePrice       *float64 `json:"DivinePrice"`
	ChaosDivinePrice  *float64 `json:"ChaosDivinePrice"`
	BaseCurrencyAPIID *string  `json:"BaseCurrencyApiId"`
}

type scoutItem struct {
	Text         string   `json:"Text"`
	Name         *string  `json:"Name"`
	Type         *string  `json:"Type"`
	CurrentPrice *float64 `json:"CurrentPrice"`
}

// ScoutSource is an aggregated-price source backed by https://poe2scout.com (currency,
// uniques, gems). Safe for concurrent use.
type ScoutSource struct {
	baseURL   *url.URL
	userAgent string
	http      *http.Client
	leagues   *otter.Cache[struct{}, []scoutLeague]
	items     *otter.Cache[string, []scoutItem]
}

// NewScoutSource creates a source; the base URL is usually `https://poe2scout.com`.
func NewScoutSource(opts SourceOptions) (*ScoutSource, error) {
	if err := checkBaseURL(opts.BaseURL); err != nil {
		return nil, err
	}
	return &ScoutSource{
		baseURL:   opts.BaseURL,
		userAgent: opts.UserAgent,
		http:      newHTTPClient(),
		leagues:   newCache[struct{}, []scoutLeague](1, opts.CacheTTL),
		items:     newCache[string, []scoutItem](16, opts.CacheTTL),
	}, nil
}

// getList GETs a JSON array below the base URL.
func getList[T any](ctx context.Context, s *ScoutSource, segments ...string) ([]T, error) {
	body, err := get(ctx, s.http, joinPath(s.baseURL, segments...), s.userAgent)
	if err != nil {
		return nil, err
	}
	var v []T
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, decodeError("%s: %v", scoutSource, err)
	}
	if v == nil {
		return nil, decodeError("%s: expected a list, got null", scoutSource)
	}
	return v, nil
}

func (s *ScoutSource) leagueList(ctx context.Context) ([]scoutLeague, error) {
	return cached(ctx, s.leagues, struct{}{}, func(ctx context.Context) ([]scoutLeague, error) {
		return getList[scoutLeague](ctx, s, scoutRealm, "Leagues")
	})
}

func (s *ScoutSource) itemList(ctx context.Context, league string) ([]scoutItem, error) {
	return cached(ctx, s.items, league, func(ctx context.Context) ([]scoutItem, error) {
		return getList[scoutItem](ctx, s, scoutRealm, "Leagues", league, "Items")
	})
}

// Rates are the exchange rates for league, base = the league's base currency (exalted).
func (s *ScoutSource) Rates(ctx context.Context, league string) (core.ExchangeRates, error) {
	leagues, err := s.leagueList(ctx)
	if err != nil {
		return core.ExchangeRates{}, err
	}
	var l *scoutLeague
	for i := range leagues {
		if leagues[i].Value == league {
			l = &leagues[i]
			break
		}
	}
	if l == nil {
		return core.ExchangeRates{}, unsupportedError(scoutSource)
	}
	rates := core.ExchangeRates{Base: "exalted", Rates: map[string]float64{}}
	if l.BaseCurrencyAPIID != nil {
		rates.Base = *l.BaseCurrencyAPIID
	}
	if div := l.DivinePrice; div != nil && *div > 0 {
		rates.Rates["divine"] = *div
		if cd := l.ChaosDivinePrice; cd != nil && *cd > 0 {
			rates.Rates["chaos"] = *div / *cd
		}
	}
	return rates, nil
}

func scoutFind(items []scoutItem, item *core.ParsedItem) *scoutItem {
	if item.Category == core.CategoryUnique {
		if item.Name == nil {
			return nil
		}
		name := *item.Name
		byName := func(i *scoutItem) bool { return i.Name != nil && *i.Name == name }
		for i := range items {
			if it := &items[i]; byName(it) && it.Type != nil && *it.Type == item.BaseType {
				return it
			}
		}
		for i := range items {
			if byName(&items[i]) {
				return &items[i]
			}
		}
		return nil
	}
	text := exchangeName(item)
	for i := range items {
		if items[i].Text == text {
			return &items[i]
		}
	}
	return nil
}

// Kind implements [PriceSource].
func (s *ScoutSource) Kind() core.PriceSourceKind { return core.SourcePoe2Scout }

// Supports implements [PriceSource].
func (s *ScoutSource) Supports(item *core.ParsedItem) bool {
	switch item.Category {
	case core.CategoryCurrency, core.CategoryGem:
		return true
	case core.CategoryUnique:
		return item.Name != nil
	default:
		return false
	}
}

// Price implements [PriceSource].
func (s *ScoutSource) Price(ctx context.Context, req *PriceRequest) (core.PriceSummary, error) {
	if !s.Supports(&req.Item) {
		return core.PriceSummary{}, unsupportedError(scoutSource)
	}
	league := req.Filters.League
	rates, err := s.Rates(ctx, league)
	if err != nil {
		return core.PriceSummary{}, err
	}
	items, err := s.itemList(ctx, league)
	if err != nil {
		return core.PriceSummary{}, err
	}
	it := scoutFind(items, &req.Item)
	if it == nil || it.CurrentPrice == nil {
		return core.PriceSummary{}, unsupportedError(scoutSource)
	}
	return aggregatedSummary(core.SourcePoe2Scout, displayPrice(*it.CurrentPrice, &rates), 1), nil
}
