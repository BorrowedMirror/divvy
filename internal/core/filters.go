package core

// Editable filters: the defaults built for an item, which the user edits in the price panel.

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/BorrowedMirror/divvy/internal/enum"
)

// QueryOptions are options from settings that influence default filters.
type QueryOptions struct {
	League string `json:"league"`
	// Initial seller filter ([StatusAny] by default in settings).
	Status OnlineStatus `json:"status"`
	// Default widening applied to stat values, in percent (e.g. 10 => min = value * 0.9).
	WidenPct float64 `json:"widen_pct"`
	// Enable all matched explicit stats by default (otherwise only the top few).
	EnableAllStats bool `json:"enable_all_stats"`
}

// OnlineStatus is the trade "status" option.
type OnlineStatus string

// OnlineStatus values.
const (
	// StatusSecurable is `securable` (instant buyout) in trade2.
	StatusSecurable OnlineStatus = "securable"
	StatusOnline    OnlineStatus = "online"
	StatusAny       OnlineStatus = "any"
)

// UnmarshalText accepts only the known statuses.
func (s *OnlineStatus) UnmarshalText(text []byte) error {
	return enum.Parse(s, text, StatusSecurable, StatusOnline, StatusAny)
}

// StatFilter is one editable stat row in the price panel.
type StatFilter struct {
	StatID string `json:"stat_id"`
	// Display text from the item.
	Text    string   `json:"text"`
	Enabled bool     `json:"enabled"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
	// Value(s) on the item, for showing beside the inputs and re-widening.
	ItemValues []float64 `json:"item_values"`
	// Mod tiers from advanced item text (1 = best), one per mod the row sums; empty when
	// unknown. May be missing when decoding.
	Tiers []int `json:"tiers"`
}

// RangeFilter is a numeric range filter for misc properties (ilvl, quality, gem level, ...).
type RangeFilter struct {
	Enabled bool     `json:"enabled"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
}

// MiscFilters are the property filters of a [FilterSet].
type MiscFilters struct {
	ItemLevel    RangeFilter `json:"item_level"`
	Quality      RangeFilter `json:"quality"`
	GemLevel     RangeFilter `json:"gem_level"`
	WaystoneTier RangeFilter `json:"waystone_tier"`
	Sockets      RangeFilter `json:"sockets"`
	Corrupted    *bool       `json:"corrupted"`
	Identified   *bool       `json:"identified"`
}

// FilterSet is everything the user can edit before (re)querying. Round-trips through IPC.
type FilterSet struct {
	League string       `json:"league"`
	Status OnlineStatus `json:"status"`
	// Exact name for uniques (`name` in trade2), base type for everything else.
	Name     *string `json:"name"`
	BaseType *string `json:"base_type"`
	// Trade category filter, e.g. `armour.helmet`.
	Category *string      `json:"category"`
	Rarity   *string      `json:"rarity"`
	Stats    []StatFilter `json:"stats"`
	Misc     MiscFilters  `json:"misc"`
	WidenPct float64      `json:"widen_pct"`
	// Route: bulk exchange (currency/fragments) vs item search.
	Route QueryRoute `json:"route"`
	// Unidentified unique: the uniques that use this base, to price one of them by name.
	// May be missing when decoding.
	UniqueCandidates []string `json:"unique_candidates"`
}

// QueryRoute selects the trade endpoint.
type QueryRoute string

// QueryRoute values.
const (
	// RouteSearch is `/api/trade2/search/{league}`.
	RouteSearch QueryRoute = "search"
	// RouteExchange is `/api/trade2/exchange/{league}` (bulk currency).
	RouteExchange QueryRoute = "exchange"
)

// UnmarshalText accepts only the known routes.
func (r *QueryRoute) UnmarshalText(text []byte) error {
	return enum.Parse(r, text, RouteSearch, RouteExchange)
}

// explicitLikeKinds are the modifier kinds enabled by default: explicit mods and their
// variants.
var explicitLikeKinds = []ModKind{ModExplicit, ModFractured, ModDesecrated, ModCrafted, ModSanctified}

// defaultEnabledStats is the number of explicit-like stats enabled by default when
// EnableAllStats is off.
const defaultEnabledStats = 4

// widenMin is the lower bound for a stat value v widened by pct percent (never above v).
//
// Integer item values produce integer bounds (floored); fractional ones keep two decimals.
func widenMin(v, pct float64, integral bool) float64 {
	pct = min(max(pct, 0), 100)
	raw := v - math.Abs(v)*pct/100
	// Small epsilon so float noise (5.5 * 0.9 = 4.949999...) does not drop a cent.
	var out float64
	if integral {
		out = math.Floor(raw + 1e-9)
	} else {
		out = math.Floor(raw*100+1e-6) / 100
	}
	return min(out, v)
}

// widenMaxNegative is the upper bound for a negative stat value v ("30% reduced ..." is
// stored as -30, and more negative is better), widened by pct percent towards zero (never
// below v).
func widenMaxNegative(v, pct float64, integral bool) float64 {
	return -widenMin(-v, pct, integral)
}

func isIntegral(v float64) bool { return v == math.Trunc(v) }

// widenBounds returns the `(min, max)` search bounds for a stat with the given item values:
// positive values get a widened minimum, negative ("reduced") values a widened maximum.
func widenBounds(values []float64, pct float64) (*float64, *float64) {
	if len(values) == 0 {
		return nil, nil
	}
	var sum float64
	integral := true
	for _, v := range values {
		sum += v
		integral = integral && isIntegral(v)
	}
	avg := sum / float64(len(values))
	integral = integral && isIntegral(avg)
	if avg < 0 {
		return nil, new(widenMaxNegative(avg, pct, integral))
	}
	return new(widenMin(avg, pct, integral)), nil
}

// searchStatID is the stat id to search with. Desecrated mods are ordinary explicit mods
// once revealed and sellers' items mostly carry them as plain explicits, so price them as
// explicit stats.
func searchStatID(id string) string {
	if rest, ok := strings.CutPrefix(id, "desecrated."); ok {
		return "explicit." + rest
	}
	return id
}

// formatNumber writes v in shortest decimal form without an exponent (`40`, `4.5`);
// negative zero is written as `0`.
func formatNumber(v float64) string {
	return strconv.FormatFloat(v+0, 'f', -1, 64)
}

// renderTemplate renders a stat template (`#% increased Spirit`) with values, e.g. for
// merged rows.
func renderTemplate(template string, values []float64) string {
	var out strings.Builder
	out.Grow(len(template))
	next := 0
	for _, ch := range template {
		if ch != '#' {
			out.WriteRune(ch)
			continue
		}
		if next >= len(values) {
			out.WriteByte('#')
			continue
		}
		out.WriteString(formatNumber(values[next]))
		next++
	}
	return out.String()
}

type templatedRow struct {
	row      StatFilter
	template string
}

// mergeSameStats: the trade site sums every mod with the same stat on an item (e.g. a
// prefix and a desecrated "% increased Spirit"), so one filter per stat with the summed
// values is what matches.
func mergeSameStats(rows []templatedRow, pct float64) []StatFilter {
	type acc struct {
		row      StatFilter
		template string
		count    int
	}
	var out []*acc
	for _, r := range rows {
		idx := slices.IndexFunc(out, func(a *acc) bool { return a.row.StatID == r.row.StatID })
		if idx >= 0 && len(out[idx].row.ItemValues) == len(r.row.ItemValues) {
			a := out[idx]
			for i := range a.row.ItemValues {
				a.row.ItemValues[i] += r.row.ItemValues[i]
			}
			a.row.Enabled = a.row.Enabled || r.row.Enabled
			a.row.Tiers = append(a.row.Tiers, r.row.Tiers...)
			a.count++
		} else {
			out = append(out, &acc{r.row, r.template, 1})
		}
	}
	res := make([]StatFilter, 0, len(out))
	for _, a := range out {
		f := a.row
		if a.count > 1 {
			f.Text = renderTemplate(a.template, f.ItemValues)
			f.Min, f.Max = widenBounds(f.ItemValues, pct)
		}
		res = append(res, f)
	}
	return res
}

func rangeOf(enabled bool, minV *uint32) RangeFilter {
	r := RangeFilter{Enabled: enabled && minV != nil}
	if minV != nil {
		r.Min = new(float64(*minV))
	}
	return r
}

// addPseudoStats prepends pseudo totals to stats and turns off the mods an enabled total
// covers.
func addPseudoStats(item *ParsedItem, stats []StatFilter, widenPct float64) []StatFilter {
	pseudo, covered := pseudoFilters(item.Modifiers, item.Class, widenPct)
	var coveredIDs []string
	for _, i := range covered {
		if id := item.Modifiers[i].StatID; id != nil {
			coveredIDs = append(coveredIDs, searchStatID(*id))
		}
	}
	for i := range stats {
		if slices.Contains(coveredIDs, stats[i].StatID) {
			stats[i].Enabled = false
		}
	}
	return append(pseudo, stats...)
}

// BuildFilters builds default filters for an item.
//
//   - uniques search by `name` + `type`, stats present but disabled;
//   - rares/magic/normal search by base `type` + trade category, `nonunique` rarity, with
//     the first four explicit-like stats enabled (all of them with
//     EnableAllStats); implicit, rune and enchant stats start disabled;
//   - unresolved magic names (affixes still in BaseType) drop the `type` filter;
//   - gems enable gem level and quality, waystones enable tier, normal/magic bases item level;
//   - currency (and uncut gems) use the bulk-exchange route.
func BuildFilters(item *ParsedItem, opts *QueryOptions) FilterSet {
	unique := item.Rarity == RarityUnique && item.Category == CategoryUnique
	currencyLike := item.Category == CategoryCurrency || item.Category == CategoryGem
	route := RouteSearch
	if item.Category == CategoryCurrency || item.Class == ClassUncutGem.Class() {
		route = RouteExchange
	}
	unresolvedMagic := item.Rarity == RarityMagic && item.Name == nil && !item.Unidentified
	var name, baseType *string
	switch {
	case unique:
		if item.Name != nil {
			name = new(*item.Name)
		}
		baseType = new(item.BaseType)
	case unresolvedMagic:
	default:
		baseType = new(item.BaseType)
	}
	var rarity *string
	switch {
	case unique:
		rarity = new("unique")
	case currencyLike || item.Category == CategoryOther:
	default:
		rarity = new("nonunique")
	}

	explicitSeen := 0
	var rows []templatedRow
	for _, m := range item.Modifiers {
		if m.Kind == ModSkill || m.StatID == nil {
			continue
		}
		explicitLike := slices.Contains(explicitLikeKinds, m.Kind)
		enabled := false
		if !unique && explicitLike {
			if opts.EnableAllStats {
				enabled = true
			} else {
				explicitSeen++
				enabled = explicitSeen <= defaultEnabledStats
			}
		}
		minV, maxV := widenBounds(m.Values, opts.WidenPct)
		tiers := []int{}
		if m.Advanced != nil && m.Advanced.Tier != nil {
			tiers = append(tiers, int(*m.Advanced.Tier))
		}
		rows = append(rows, templatedRow{
			row: StatFilter{
				StatID:     searchStatID(*m.StatID),
				Text:       m.Text,
				Enabled:    enabled,
				Min:        minV,
				Max:        maxV,
				ItemValues: slices.Clone(m.Values),
				Tiers:      tiers,
			},
			template: m.Template,
		})
	}
	stats := mergeSameStats(rows, opts.WidenPct)
	// Totals the trade site sums across mods (resistances, life, ...): enabled pseudo rows go
	// first and replace the mods they cover, which stay listed but start disabled.
	if !unique && !currencyLike {
		stats = addPseudoStats(item, stats, opts.WidenPct)
	}

	gem := item.Category == CategoryGem
	baseItem := (item.Rarity == RarityNormal || item.Rarity == RarityMagic) &&
		(item.Category == CategoryEquipment || item.Category == CategoryJewel)
	var sockets *uint32
	if item.Sockets > 0 {
		sockets = new(uint32(item.Sockets))
	}
	misc := MiscFilters{
		ItemLevel: rangeOf(baseItem, item.ItemLevel),
		Quality:   rangeOf(gem, item.Quality),
		GemLevel:  rangeOf(gem, item.GemLevel),
		WaystoneTier: RangeFilter{
			Enabled: item.WaystoneTier != nil,
		},
		Sockets: rangeOf(false, sockets),
	}
	if item.WaystoneTier != nil {
		misc.WaystoneTier.Min = new(float64(*item.WaystoneTier))
		misc.WaystoneTier.Max = new(float64(*item.WaystoneTier))
	}
	switch {
	case item.Corrupted:
		misc.Corrupted = new(true)
	case gem || unique:
		misc.Corrupted = new(false)
	}
	if item.Unidentified {
		misc.Identified = new(false)
	}

	var category *string
	if c, ok := item.Class.tradeCategory(); ok {
		category = &c
	}
	return FilterSet{
		League:           opts.League,
		Status:           opts.Status,
		Name:             name,
		BaseType:         baseType,
		Category:         category,
		Rarity:           rarity,
		Stats:            stats,
		Misc:             misc,
		WidenPct:         opts.WidenPct,
		Route:            route,
		UniqueCandidates: []string{},
	}
}
