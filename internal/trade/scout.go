package trade

// poe2scout aggregated prices (`https://api.poe2scout.com`, see docs/research/ninja-scout.md).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/maypok86/otter/v2"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

const (
	scoutSource = "poe2scout"
	scoutRealm  = "poe2"
	// scoutDefaultTTL: poe2scout refreshes far less often than this.
	scoutDefaultTTL = 600 * time.Second
)

type scoutLeague struct {
	value             string
	divinePrice       *float64
	chaosDivinePrice  *float64
	baseCurrencyAPIID *string
}

func (l *scoutLeague) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ScoutLeague")
	if err != nil {
		return err
	}
	var v scoutLeague
	if err := o.required("Value", &v.value); err != nil {
		return err
	}
	if err := o.optional("DivinePrice", &v.divinePrice); err != nil {
		return err
	}
	if err := o.optional("ChaosDivinePrice", &v.chaosDivinePrice); err != nil {
		return err
	}
	if err := o.optional("BaseCurrencyApiId", &v.baseCurrencyAPIID); err != nil {
		return err
	}
	*l = v
	return nil
}

type scoutItem struct {
	text         string
	name         *string
	kind         *string
	currentPrice *float64
}

func (i *scoutItem) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ScoutItem")
	if err != nil {
		return err
	}
	var v scoutItem
	if err := o.required("Text", &v.text); err != nil {
		return err
	}
	if err := o.optional("Name", &v.name); err != nil {
		return err
	}
	if err := o.optional("Type", &v.kind); err != nil {
		return err
	}
	if err := o.optional("CurrentPrice", &v.currentPrice); err != nil {
		return err
	}
	*i = v
	return nil
}

// Poe2ScoutSource is an aggregated-price source backed by https://poe2scout.com (currency,
// uniques, gems). Safe for concurrent use.
type Poe2ScoutSource struct {
	BaseURL   *url.URL
	UserAgent string
	http      *http.Client
	leagues   *otter.Cache[struct{}, []scoutLeague]
	items     *otter.Cache[string, []scoutItem]
}

// NewPoe2ScoutSource creates a source with a 10-minute cache.
func NewPoe2ScoutSource(baseURL *url.URL, userAgent string) *Poe2ScoutSource {
	return NewPoe2ScoutSourceWithCacheTTL(baseURL, userAgent, scoutDefaultTTL)
}

// NewPoe2ScoutSourceWithCacheTTL is like [NewPoe2ScoutSource] with a custom cache lifetime
// for the league/item lists (0 disables caching).
func NewPoe2ScoutSourceWithCacheTTL(baseURL *url.URL, userAgent string, ttl time.Duration) *Poe2ScoutSource {
	return &Poe2ScoutSource{
		BaseURL:   baseURL,
		UserAgent: userAgent,
		http:      newHTTPClient(0),
		leagues:   newCache[struct{}, []scoutLeague](1, ttl),
		items:     newCache[string, []scoutItem](16, ttl),
	}
}

func (s *Poe2ScoutSource) url(segments ...string) *url.URL {
	if cannotBeABase(s.BaseURL) {
		if s.BaseURL == nil {
			return &url.URL{}
		}
		u := *s.BaseURL
		return &u
	}
	return appendSegments(s.BaseURL, segments...)
}

func (s *Poe2ScoutSource) getJSON(ctx context.Context, u *url.URL, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return TransportError(err)
	}
	req.Header.Set("User-Agent", s.UserAgent)
	status, body, err := getBody(ctx, s.http, req)
	if err != nil {
		return err
	}
	if !isSuccess(status) {
		return HTTPError(uint16(status), firstChars(lossy(body), 200))
	}
	if isNull(body) {
		// serde rejects null for a Vec; encoding/json would decode it as nil.
		return DecodeError(scoutSource + ": invalid type: null, expected a sequence")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return DecodeError(fmt.Sprintf("%s: %v", scoutSource, err))
	}
	return nil
}

func (s *Poe2ScoutSource) leagueList(ctx context.Context) ([]scoutLeague, error) {
	if v, ok := cacheGet(s.leagues, struct{}{}); ok {
		return v, nil
	}
	var v []scoutLeague
	if err := s.getJSON(ctx, s.url(scoutRealm, "Leagues"), &v); err != nil {
		return nil, err
	}
	if v == nil {
		v = []scoutLeague{}
	}
	cacheSet(s.leagues, struct{}{}, v)
	return v, nil
}

func (s *Poe2ScoutSource) itemList(ctx context.Context, league string) ([]scoutItem, error) {
	if v, ok := cacheGet(s.items, league); ok {
		return v, nil
	}
	var v []scoutItem
	if err := s.getJSON(ctx, s.url(scoutRealm, "Leagues", league, "Items"), &v); err != nil {
		return nil, err
	}
	if v == nil {
		v = []scoutItem{}
	}
	cacheSet(s.items, league, v)
	return v, nil
}

// Rates are the exchange rates for league, base = the league's base currency (exalted).
func (s *Poe2ScoutSource) Rates(ctx context.Context, league string) (core.ExchangeRates, error) {
	leagues, err := s.leagueList(ctx)
	if err != nil {
		return core.ExchangeRates{}, err
	}
	var l *scoutLeague
	for i := range leagues {
		if leagues[i].value == league {
			l = &leagues[i]
			break
		}
	}
	if l == nil {
		return core.ExchangeRates{}, UnsupportedError(scoutSource)
	}
	rates := core.ExchangeRates{Base: "exalted", Rates: map[string]float64{}}
	if l.baseCurrencyAPIID != nil {
		rates.Base = *l.baseCurrencyAPIID
	}
	if div := l.divinePrice; div != nil && *div > 0 {
		rates.Rates["divine"] = *div
		if cd := l.chaosDivinePrice; cd != nil && *cd > 0 {
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
		byName := func(i *scoutItem) bool { return i.name != nil && *i.name == name }
		for i := range items {
			if it := &items[i]; byName(it) && it.kind != nil && *it.kind == item.BaseType {
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
	text := ExchangeName(item)
	for i := range items {
		if items[i].text == text {
			return &items[i]
		}
	}
	return nil
}

// Kind implements [PriceSource].
func (s *Poe2ScoutSource) Kind() core.PriceSourceKind { return core.SourcePoe2Scout }

// Supports implements [PriceSource].
func (s *Poe2ScoutSource) Supports(item *core.ParsedItem) bool {
	switch item.Category {
	case core.CategoryCurrency, core.CategoryGem:
		return true
	case core.CategoryUnique:
		return item.Name != nil
	}
	return false
}

// Price implements [PriceSource].
func (s *Poe2ScoutSource) Price(ctx context.Context, req *PriceRequest) (core.PriceSummary, error) {
	if !s.Supports(&req.Item) {
		return core.PriceSummary{}, UnsupportedError(scoutSource)
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
	if it == nil || it.currentPrice == nil {
		return core.PriceSummary{}, UnsupportedError(scoutSource)
	}
	return AggregatedSummary(core.SourcePoe2Scout, DisplayPrice(*it.currentPrice, &rates), 1), nil
}
