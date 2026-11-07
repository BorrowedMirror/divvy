package trade

// poe.ninja PoE2 economy API (see docs/research/ninja-scout.md).
//
// Currency-like items come from `/poe2/api/economy/exchange/current/overview`, uniques from
// `/poe2/api/economy/stash/current/item/overview`. Values are in `core.primary` (divine) and
// `core.rates[c]` is how many c one primary buys.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/maypok86/otter/v2"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// ninjaDefaultTTL: poe.ninja refreshes PoE2 overviews roughly hourly; ten minutes keeps us
// well inside their "don't hammer the economy endpoints" policy.
const ninjaDefaultTTL = 600 * time.Second

const ninjaSource = "poe.ninja"

const (
	ninjaExchangePath = "poe2/api/economy/exchange/current/overview"
	ninjaStashPath    = "poe2/api/economy/stash/current/item/overview"
)

// allExchange is every exchange overview type, in lookup order for unknown currency-like
// classes.
var allExchange = []string{
	"Currency", "Fragments", "Runes", "Essences", "SoulCores", "Ritual", "Breach", "Delirium",
	"Expedition", "Abyss", "Verisium", "Idols", "UncutGems", "LineageSupportGems",
}

var allStash = []string{
	"UniqueAccessories", "UniqueArmours", "UniqueWeapons", "UniqueFlasks", "UniqueCharms",
	"UniqueJewels", "UniqueSanctumRelics", "UniqueTablets", "PrecursorTablets",
}

type ninjaEndpoint int

const (
	endpointExchange ninjaEndpoint = iota
	endpointStash
)

func (e ninjaEndpoint) path() string {
	if e == endpointStash {
		return ninjaStashPath
	}
	return ninjaExchangePath
}

type ninjaCore struct {
	primary string
	rates   map[string]float64
}

func (c *ninjaCore) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct Core")
	if err != nil {
		return err
	}
	var v ninjaCore
	if err := o.required("primary", &v.primary); err != nil {
		return err
	}
	if err := o.defaulted("rates", &v.rates); err != nil {
		return err
	}
	*c = v
	return nil
}

type ninjaLine struct {
	// String trade id on exchange overviews, numeric on stash overviews.
	id           any
	primaryValue float64
	name         *string
	baseType     *string
	listingCount *uint32
}

func (l *ninjaLine) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct Line")
	if err != nil {
		return err
	}
	var v ninjaLine
	if err := o.present("id", &v.id); err != nil {
		return err
	}
	if err := o.required("primaryValue", &v.primaryValue); err != nil {
		return err
	}
	if err := o.optional("name", &v.name); err != nil {
		return err
	}
	if err := o.optional("baseType", &v.baseType); err != nil {
		return err
	}
	if err := o.optional("listingCount", &v.listingCount); err != nil {
		return err
	}
	*l = v
	return nil
}

type ninjaExchangeItem struct {
	id, name string
}

func (i *ninjaExchangeItem) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ExchangeItem")
	if err != nil {
		return err
	}
	var v ninjaExchangeItem
	if err := o.required("id", &v.id); err != nil {
		return err
	}
	if err := o.required("name", &v.name); err != nil {
		return err
	}
	*i = v
	return nil
}

type ninjaOverview struct {
	core  ninjaCore
	lines []ninjaLine
	items []ninjaExchangeItem
}

func (ov *ninjaOverview) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct Overview")
	if err != nil {
		return err
	}
	var v ninjaOverview
	if err := o.required("core", &v.core); err != nil {
		return err
	}
	if err := o.defaulted("lines", &v.lines); err != nil {
		return err
	}
	if err := o.defaulted("items", &v.items); err != nil {
		return err
	}
	*ov = v
	return nil
}

func (ov *ninjaOverview) rates() core.ExchangeRates {
	rates := map[string]float64{}
	for k, v := range ov.core.rates {
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 {
			rates[k] = 1 / v
		}
	}
	return core.ExchangeRates{Base: ov.core.primary, Rates: rates}
}

// asciiEqualFold is `str::eq_ignore_ascii_case`.
func asciiEqualFold(a, b string) bool {
	return len(a) == len(b) && asciiLower(a) == asciiLower(b)
}

// find returns the value (in core.primary) and listing count for item, if listed.
func (ov *ninjaOverview) find(endpoint ninjaEndpoint, item *core.ParsedItem) (float64, uint32, bool) {
	if endpoint == endpointExchange {
		name := ExchangeName(item)
		for _, it := range ov.items {
			if !asciiEqualFold(it.name, name) {
				continue
			}
			for _, l := range ov.lines {
				if s, ok := l.id.(string); ok && s == it.id {
					return l.primaryValue, 1, true
				}
			}
			return 0, 0, false
		}
		return 0, 0, false
	}
	if item.Name == nil {
		return 0, 0, false
	}
	name := *item.Name
	byName := func(l *ninjaLine) bool { return l.name != nil && *l.name == name }
	var hit *ninjaLine
	for i := range ov.lines {
		if l := &ov.lines[i]; byName(l) && l.baseType != nil && *l.baseType == item.BaseType {
			hit = l
			break
		}
	}
	if hit == nil {
		for i := range ov.lines {
			if byName(&ov.lines[i]) {
				hit = &ov.lines[i]
				break
			}
		}
	}
	if hit == nil {
		return 0, 0, false
	}
	count := uint32(1)
	if hit.listingCount != nil {
		count = *hit.listingCount
	}
	return hit.primaryValue, count, true
}

// exchangeTypes are the exchange overview types to try for a currency-like item class.
func exchangeTypes(class core.ItemClass) []string {
	switch class.Kind {
	case core.ClassStackableCurrency:
		return []string{"Currency", "Fragments", "Expedition", "Verisium", "Abyss", "Ritual", "Breach", "Delirium", "Idols"}
	case core.ClassOmen:
		return []string{"Ritual"}
	case core.ClassEssence:
		return []string{"Essences"}
	case core.ClassSoulCore:
		return []string{"SoulCores"}
	case core.ClassRune:
		return []string{"Runes"}
	case core.ClassTalisman:
		return []string{"Runes", "Idols"}
	case core.ClassDistilledEmotion:
		return []string{"Delirium"}
	case core.ClassSplinter:
		return []string{"Breach", "Fragments", "Delirium", "Ritual"}
	case core.ClassCatalyst:
		return []string{"Breach"}
	case core.ClassUncutGem:
		return []string{"UncutGems"}
	case core.ClassSupportGem:
		return []string{"LineageSupportGems"}
	}
	return allExchange
}

// stashTypes are the stash overview types to try for a unique of this class.
func stashTypes(class core.ItemClass) []string {
	switch class.Kind {
	case core.ClassAmulet, core.ClassRing, core.ClassBelt:
		return []string{"UniqueAccessories"}
	case core.ClassBodyArmour, core.ClassHelmet, core.ClassGloves, core.ClassBoots, core.ClassShield,
		core.ClassBuckler, core.ClassFocus:
		return []string{"UniqueArmours"}
	case core.ClassQuiver:
		return []string{"UniqueArmours", "UniqueWeapons"}
	case core.ClassWeapon:
		return []string{"UniqueWeapons"}
	case core.ClassLifeFlask, core.ClassManaFlask:
		return []string{"UniqueFlasks"}
	case core.ClassCharm:
		return []string{"UniqueCharms"}
	case core.ClassJewel:
		return []string{"UniqueJewels"}
	case core.ClassRelic:
		return []string{"UniqueSanctumRelics"}
	case core.ClassTablet:
		return []string{"UniqueTablets", "PrecursorTablets"}
	}
	return allStash
}

type ninjaCandidate struct {
	endpoint ninjaEndpoint
	kind     string
}

func ninjaCandidates(item *core.ParsedItem) []ninjaCandidate {
	endpoint, types := endpointExchange, exchangeTypes(item.Class)
	if item.Category == core.CategoryUnique {
		endpoint, types = endpointStash, stashTypes(item.Class)
	}
	out := make([]ninjaCandidate, len(types))
	for i, t := range types {
		out[i] = ninjaCandidate{endpoint, t}
	}
	return out
}

type ninjaKey struct{ league, kind string }

// PoeNinjaSource is an aggregated-price source backed by https://poe.ninja (currency,
// uniques, gems). Safe for concurrent use.
type PoeNinjaSource struct {
	BaseURL   *url.URL
	UserAgent string
	http      *http.Client
	cache     *otter.Cache[ninjaKey, *ninjaOverview]
}

// NewPoeNinjaSource creates a source with a 10-minute response cache.
func NewPoeNinjaSource(baseURL *url.URL, userAgent string) *PoeNinjaSource {
	return &PoeNinjaSource{
		BaseURL:   baseURL,
		UserAgent: userAgent,
		http:      newHTTPClient(0),
		cache:     newCache[ninjaKey, *ninjaOverview](64, ninjaDefaultTTL),
	}
}

// WithCacheTTL replaces the response cache with one using ttl (default 10 minutes; 0
// disables caching) and returns s. Call it before sharing s.
func (s *PoeNinjaSource) WithCacheTTL(ttl time.Duration) *PoeNinjaSource {
	s.cache = newCache[ninjaKey, *ninjaOverview](64, ttl)
	return s
}

func (s *PoeNinjaSource) url(endpoint ninjaEndpoint, league, kind string) (*url.URL, error) {
	if cannotBeABase(s.BaseURL) {
		return nil, ConfigError("relative URL with a cannot-be-a-base base")
	}
	u := s.BaseURL.ResolveReference(&url.URL{Path: endpoint.path()})
	u.RawQuery = ""
	u.Fragment = ""
	appendQuery(u, [2]string{"league", league}, [2]string{"type", kind})
	return u, nil
}

func (s *PoeNinjaSource) overview(ctx context.Context, endpoint ninjaEndpoint, league, kind string) (*ninjaOverview, error) {
	key := ninjaKey{league, endpoint.path() + "/" + kind}
	if hit, ok := cacheGet(s.cache, key); ok {
		return hit, nil
	}
	u, err := s.url(endpoint, league, kind)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, TransportError(err)
	}
	req.Header.Set("User-Agent", s.UserAgent)
	resp, body, err := getBody(ctx, s.http, req)
	if err != nil {
		return nil, err
	}
	if !isSuccess(resp.StatusCode) {
		return nil, HTTPError(uint16(resp.StatusCode), firstChars(lossy(body), 200))
	}
	var parsed ninjaOverview
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, DecodeError(fmt.Sprintf("%s %s: %v", ninjaSource, kind, err))
	}
	cacheSet(s.cache, key, &parsed)
	return &parsed, nil
}

// Rates are the current exchange rates for league (base = divine), from the Currency
// overview.
func (s *PoeNinjaSource) Rates(ctx context.Context, league string) (core.ExchangeRates, error) {
	ov, err := s.overview(ctx, endpointExchange, league, "Currency")
	if err != nil {
		return core.ExchangeRates{}, err
	}
	return ov.rates(), nil
}

// Kind implements [PriceSource].
func (s *PoeNinjaSource) Kind() core.PriceSourceKind { return core.SourcePoeNinja }

// Supports implements [PriceSource].
func (s *PoeNinjaSource) Supports(item *core.ParsedItem) bool {
	switch item.Category {
	case core.CategoryCurrency:
		return true
	case core.CategoryUnique:
		return item.Name != nil
	case core.CategoryGem:
		return item.Class.Kind == core.ClassUncutGem || item.Class.Kind == core.ClassSupportGem
	}
	return false
}

// Price implements [PriceSource].
func (s *PoeNinjaSource) Price(ctx context.Context, req *PriceRequest) (core.PriceSummary, error) {
	item := &req.Item
	if !s.Supports(item) {
		return core.PriceSummary{}, UnsupportedError(ninjaSource)
	}
	for _, c := range ninjaCandidates(item) {
		ov, err := s.overview(ctx, c.endpoint, req.Filters.League, c.kind)
		if err != nil {
			return core.PriceSummary{}, err
		}
		if value, total, ok := ov.find(c.endpoint, item); ok {
			rates := ov.rates()
			return AggregatedSummary(core.SourcePoeNinja, DisplayPrice(value, &rates), total), nil
		}
	}
	return core.PriceSummary{}, UnsupportedError(ninjaSource)
}
