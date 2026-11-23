package trade

// poe.ninja PoE2 economy API (see docs/research/ninja-scout.md).
//
// Currency-like items come from `/poe2/api/economy/exchange/current/overview`, uniques from
// `/poe2/api/economy/stash/current/item/overview`. Values are in `core.primary` (divine) and
// `core.rates[c]` is how many c one primary buys.

import (
	"context"
	"encoding/json/v2"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/maypok86/otter/v2"

	"github.com/BorrowedMirror/divvy/internal/core"
)

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

type ninjaOverview struct {
	Core struct {
		Primary string `json:"primary"`
		// Rates[c] is how many c one primary buys.
		Rates map[string]float64 `json:"rates"`
	} `json:"core"`
	Lines []ninjaLine         `json:"lines"`
	Items []ninjaExchangeItem `json:"items"`
}

type ninjaLine struct {
	// ID is the string trade id on exchange overviews, a number on stash overviews.
	ID           any     `json:"id"`
	PrimaryValue float64 `json:"primaryValue"`
	Name         *string `json:"name"`
	BaseType     *string `json:"baseType"`
	ListingCount *uint32 `json:"listingCount"`
}

type ninjaExchangeItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (ov *ninjaOverview) rates() core.ExchangeRates {
	rates := map[string]float64{}
	for k, v := range ov.Core.Rates {
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 {
			rates[k] = 1 / v
		}
	}
	return core.ExchangeRates{Base: ov.Core.Primary, Rates: rates}
}

// find returns the value (in core.primary) and listing count for item, if listed.
func (ov *ninjaOverview) find(endpoint ninjaEndpoint, item *core.ParsedItem) (float64, uint32, bool) {
	if endpoint == endpointExchange {
		name := exchangeName(item)
		for _, it := range ov.Items {
			if !strings.EqualFold(it.Name, name) {
				continue
			}
			for _, l := range ov.Lines {
				if id, ok := l.ID.(string); ok && id == it.ID {
					return l.PrimaryValue, 1, true
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
	byName := func(l *ninjaLine) bool { return l.Name != nil && *l.Name == name }
	var hit *ninjaLine
	for i := range ov.Lines {
		if l := &ov.Lines[i]; byName(l) && l.BaseType != nil && *l.BaseType == item.BaseType {
			hit = l
			break
		}
	}
	if hit == nil {
		for i := range ov.Lines {
			if byName(&ov.Lines[i]) {
				hit = &ov.Lines[i]
				break
			}
		}
	}
	if hit == nil {
		return 0, 0, false
	}
	count := uint32(1)
	if hit.ListingCount != nil {
		count = *hit.ListingCount
	}
	return hit.PrimaryValue, count, true
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
	default:
		return allExchange
	}
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
	default:
		return allStash
	}
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

// NinjaSource is an aggregated-price source backed by https://poe.ninja (currency, uniques,
// gems). Safe for concurrent use.
type NinjaSource struct {
	baseURL   *url.URL
	userAgent string
	http      *http.Client
	cache     *otter.Cache[ninjaKey, *ninjaOverview]
}

// NewNinjaSource creates a source; the base URL is usually `https://poe.ninja`.
func NewNinjaSource(opts SourceOptions) (*NinjaSource, error) {
	if err := checkBaseURL(opts.BaseURL); err != nil {
		return nil, err
	}
	return &NinjaSource{
		baseURL:   opts.BaseURL,
		userAgent: opts.UserAgent,
		http:      newHTTPClient(),
		cache:     newCache[ninjaKey, *ninjaOverview](64, opts.CacheTTL),
	}, nil
}

func (s *NinjaSource) overview(ctx context.Context, endpoint ninjaEndpoint, league, kind string) (*ninjaOverview, error) {
	key := ninjaKey{league, endpoint.path() + "/" + kind}
	return cached(ctx, s.cache, key, func(ctx context.Context) (*ninjaOverview, error) {
		u := s.baseURL.JoinPath(endpoint.path())
		u.RawQuery = url.Values{"league": {league}, "type": {kind}}.Encode()
		u.Fragment = ""
		body, err := get(ctx, s.http, u, s.userAgent)
		if err != nil {
			return nil, err
		}
		var parsed ninjaOverview
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, decodeError("%s %s: %v", ninjaSource, kind, err)
		}
		if parsed.Core.Primary == "" {
			return nil, decodeError("%s %s: no core currency", ninjaSource, kind)
		}
		return &parsed, nil
	})
}

// Rates are the current exchange rates for league (base = divine), from the Currency
// overview.
func (s *NinjaSource) Rates(ctx context.Context, league string) (core.ExchangeRates, error) {
	ov, err := s.overview(ctx, endpointExchange, league, "Currency")
	if err != nil {
		return core.ExchangeRates{}, err
	}
	return ov.rates(), nil
}

// Kind implements [PriceSource].
func (s *NinjaSource) Kind() core.PriceSourceKind { return core.SourcePoeNinja }

// Supports implements [PriceSource].
func (s *NinjaSource) Supports(item *core.ParsedItem) bool {
	switch item.Category {
	case core.CategoryCurrency:
		return true
	case core.CategoryUnique:
		return item.Name != nil
	case core.CategoryGem:
		return item.Class.Kind == core.ClassUncutGem || item.Class.Kind == core.ClassSupportGem
	default:
		return false
	}
}

// Price implements [PriceSource].
func (s *NinjaSource) Price(ctx context.Context, req *PriceRequest) (core.PriceSummary, error) {
	item := &req.Item
	if !s.Supports(item) {
		return core.PriceSummary{}, unsupportedError(ninjaSource)
	}
	for _, c := range ninjaCandidates(item) {
		ov, err := s.overview(ctx, c.endpoint, req.Filters.League, c.kind)
		if err != nil {
			return core.PriceSummary{}, err
		}
		if value, total, ok := ov.find(c.endpoint, item); ok {
			rates := ov.rates()
			return aggregatedSummary(core.SourcePoeNinja, displayPrice(value, &rates), total), nil
		}
	}
	return core.PriceSummary{}, unsupportedError(ninjaSource)
}
