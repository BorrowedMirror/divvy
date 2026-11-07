package core

// Editable filters and trade2 search query construction.

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strings"
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

// MarshalJSON writes serde-compatible JSON.
func (q QueryOptions) MarshalJSON() ([]byte, error) { return marshalStruct(q) }

// OnlineStatus is the trade "status" option.
type OnlineStatus string

// OnlineStatus values.
const (
	// StatusSecurable is `securable` (instant buyout) in trade2.
	StatusSecurable OnlineStatus = "securable"
	StatusOnline    OnlineStatus = "online"
	StatusAny       OnlineStatus = "any"
)

// UnmarshalJSON accepts only the known variants, like serde.
func (s *OnlineStatus) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, s, "OnlineStatus", StatusSecurable, StatusOnline, StatusAny)
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
	// unknown. Optional when decoding (`#[serde(default)]`).
	Tiers []uint8 `json:"tiers"`
}

// MarshalJSON writes serde-compatible JSON.
func (s StatFilter) MarshalJSON() ([]byte, error) { return marshalStruct(s) }

// UnmarshalJSON decodes the serde form (tiers as a number array, not base64).
func (s *StatFilter) UnmarshalJSON(data []byte) error {
	type plain StatFilter
	var raw struct {
		plain
		Tiers []uint16 `json:"tiers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = StatFilter(raw.plain)
	s.Tiers = nil
	if raw.Tiers != nil {
		s.Tiers = make([]uint8, len(raw.Tiers))
		for i, t := range raw.Tiers {
			if t > math.MaxUint8 {
				return &json.UnmarshalTypeError{Value: "number", Field: "tiers"}
			}
			s.Tiers[i] = uint8(t)
		}
	}
	return nil
}

// RangeFilter is a numeric range filter for misc properties (ilvl, quality, gem level, ...).
type RangeFilter struct {
	Enabled bool     `json:"enabled"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
}

// MarshalJSON writes serde-compatible JSON.
func (r RangeFilter) MarshalJSON() ([]byte, error) { return marshalStruct(r) }

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

// MarshalJSON writes serde-compatible JSON.
func (m MiscFilters) MarshalJSON() ([]byte, error) { return marshalStruct(m) }

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
	// Optional when decoding (`#[serde(default)]`).
	UniqueCandidates []string `json:"unique_candidates"`
}

// MarshalJSON writes serde-compatible JSON.
func (f FilterSet) MarshalJSON() ([]byte, error) { return marshalStruct(f) }

// QueryRoute selects the trade endpoint.
type QueryRoute string

// QueryRoute values.
const (
	// RouteSearch is `/api/trade2/search/{league}`.
	RouteSearch QueryRoute = "search"
	// RouteExchange is `/api/trade2/exchange/{league}` (bulk currency).
	RouteExchange QueryRoute = "exchange"
)

// UnmarshalJSON accepts only the known variants, like serde.
func (r *QueryRoute) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, r, "QueryRoute", RouteSearch, RouteExchange)
}

// TradeQuery is the body sent to `/api/trade2/search/{league}`. Serialises to the exact
// trade2 JSON shape.
//
// Query and Sort hold a serde_json::Value equivalent: nil, bool, float64, json.Number,
// string, []any or map[string]any (written with sorted keys, like serde_json's BTreeMap).
type TradeQuery struct {
	Query any `json:"query"`
	Sort  any `json:"sort"`
}

// MarshalJSON writes serde-compatible JSON.
func (t TradeQuery) MarshalJSON() ([]byte, error) { return marshalStruct(t) }

// UnmarshalJSON keeps numbers as json.Number so integers re-serialise as integers, like a
// serde_json::Value.
func (t *TradeQuery) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	type plain TradeQuery
	var p plain
	if err := dec.Decode(&p); err != nil {
		return err
	}
	*t = TradeQuery(p)
	return nil
}

// DefaultEnabledStats is the number of explicit-like stats enabled by default when
// EnableAllStats is off.
const DefaultEnabledStats = 4

// WidenMin is the lower bound for a stat value v widened by pct percent (never above v).
//
// Integer item values produce integer bounds (floored); fractional ones keep two decimals.
func WidenMin(v, pct float64, integral bool) float64 {
	pct = clampF64(pct, 0, 100)
	raw := v - math.Abs(v)*pct/100
	// Small epsilon so float noise (5.5 * 0.9 = 4.949999...) does not drop a cent.
	var out float64
	if integral {
		out = math.Floor(raw + 1e-9)
	} else {
		out = math.Floor(raw*100+1e-6) / 100
	}
	return minF64(out, v)
}

// WidenMaxNegative is the upper bound for a negative stat value v ("30% reduced ..." is
// stored as -30, and more negative is better), widened by pct percent towards zero (never
// below v).
func WidenMaxNegative(v, pct float64, integral bool) float64 {
	return -WidenMin(-v, pct, integral)
}

// WidenBounds returns the `(min, max)` search bounds for a stat with the given item values:
// positive values get a widened minimum, negative ("reduced") values a widened maximum.
func WidenBounds(values []float64, pct float64) (*float64, *float64) {
	if len(values) == 0 {
		return nil, nil
	}
	var sum float64
	integral := true
	for _, v := range values {
		sum += v
		if fract(v) != 0 {
			integral = false
		}
	}
	avg := sum / float64(len(values))
	integral = integral && fract(avg) == 0
	if avg < 0 {
		return nil, ptr(WidenMaxNegative(avg, pct, integral))
	}
	return ptr(WidenMin(avg, pct, integral)), nil
}

// SearchStatID is the stat id to search with. Desecrated mods are ordinary explicit mods
// once revealed and sellers' items mostly carry them as plain explicits, so price them as
// explicit stats.
func SearchStatID(id string) string {
	if rest, ok := strings.CutPrefix(id, "desecrated."); ok {
		return "explicit." + rest
	}
	return id
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
		v := values[next]
		next++
		if fract(v) == 0 {
			out.WriteString(formatI64(f64ToI64(v)))
		} else {
			out.WriteString(formatF64Display(v))
		}
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
			f.Min, f.Max = WidenBounds(f.ItemValues, pct)
		}
		res = append(res, f)
	}
	return res
}

func rangeOf(enabled bool, minV *uint32) RangeFilter {
	r := RangeFilter{Enabled: enabled && minV != nil}
	if minV != nil {
		r.Min = ptr(float64(*minV))
	}
	return r
}

// addPseudoStats prepends pseudo totals to stats and turns off the mods an enabled total
// covers.
func addPseudoStats(item *ParsedItem, stats []StatFilter, widenPct float64) []StatFilter {
	pseudo, covered := PseudoFilters(item.Modifiers, item.Class, widenPct)
	var coveredIDs []string
	for _, i := range covered {
		if id := item.Modifiers[i].StatID; id != nil {
			coveredIDs = append(coveredIDs, SearchStatID(*id))
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
//     the first [DefaultEnabledStats] explicit-like stats enabled (all of them with
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
		name, baseType = clonePtr(item.Name), ptr(item.BaseType)
	case unresolvedMagic:
	default:
		baseType = ptr(item.BaseType)
	}
	var rarity *string
	switch {
	case unique:
		rarity = ptr("unique")
	case currencyLike || item.Category == CategoryOther:
	default:
		rarity = ptr("nonunique")
	}

	explicitSeen := 0
	var rows []templatedRow
	for _, m := range item.Modifiers {
		if m.Kind == ModSkill || m.StatID == nil {
			continue
		}
		explicitLike := false
		switch m.Kind {
		case ModExplicit, ModFractured, ModDesecrated, ModCrafted, ModSanctified:
			explicitLike = true
		}
		enabled := false
		if !unique && explicitLike {
			if opts.EnableAllStats {
				enabled = true
			} else {
				explicitSeen++
				enabled = explicitSeen <= DefaultEnabledStats
			}
		}
		minV, maxV := WidenBounds(m.Values, opts.WidenPct)
		tiers := []uint8{}
		if m.Advanced != nil && m.Advanced.Tier != nil {
			tiers = append(tiers, *m.Advanced.Tier)
		}
		rows = append(rows, templatedRow{
			row: StatFilter{
				StatID:     SearchStatID(*m.StatID),
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
		sockets = ptr(uint32(item.Sockets))
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
		misc.WaystoneTier.Min = ptr(float64(*item.WaystoneTier))
		misc.WaystoneTier.Max = ptr(float64(*item.WaystoneTier))
	}
	switch {
	case item.Corrupted:
		misc.Corrupted = ptr(true)
	case gem || unique:
		misc.Corrupted = ptr(false)
	}
	if item.Unidentified {
		misc.Identified = ptr(false)
	}

	var category *string
	if c, ok := item.Class.TradeCategory(); ok {
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

func rangeJSON(r *RangeFilter) (any, bool) {
	if !r.Enabled || (r.Min == nil && r.Max == nil) {
		return nil, false
	}
	return minMax(r.Min, r.Max), true
}

func minMax(minV, maxV *float64) map[string]any {
	m := map[string]any{}
	if minV != nil {
		m["min"] = *minV
	}
	if maxV != nil {
		m["max"] = *maxV
	}
	return m
}

func boolOption(v bool) map[string]any {
	if v {
		return map[string]any{"option": "true"}
	}
	return map[string]any{"option": "false"}
}

type groupEntry struct {
	key   string
	value any
	ok    bool
}

// addGroup inserts `{ "<group>": { "filters": { ... } } }` when the group has entries.
func addGroup(out map[string]any, name string, entries []groupEntry) {
	filters := map[string]any{}
	for _, e := range entries {
		if e.ok {
			filters[e.key] = e.value
		}
	}
	if len(filters) > 0 {
		out[name] = map[string]any{"filters": filters}
	}
}

// Rewiden re-applies pct to every stat's min/max from its item values.
func (f *FilterSet) Rewiden(pct float64) {
	f.WidenPct = pct
	for i := range f.Stats {
		s := &f.Stats[i]
		if len(s.ItemValues) > 0 {
			s.Min, s.Max = WidenBounds(s.ItemValues, pct)
		}
	}
}

// ToTradeQuery serialises to the trade2 search body.
//
// Shape (verified against pathofexile.com/trade2 requests and `/api/trade2/data/filters`):
//
//	{
//	  "query": {
//	    "status": { "option": "securable" | "online" | "any" },
//	    "name": "Goldrim",               // uniques only
//	    "type": "Expert Hunter Hood",    // base type
//	    "stats": [{ "type": "and", "filters": [
//	      { "id": "explicit.stat_3299347043", "value": { "min": 38 }, "disabled": false }
//	    ]}],
//	    "filters": {
//	      "type_filters":      { "filters": { "category": { "option": "armour.helmet" },
//	                                          "rarity": { "option": "nonunique" },
//	                                          "ilvl": { "min": 79 }, "quality": { "min": 20 } } },
//	      "misc_filters":      { "filters": { "gem_level": { "min": 20 },
//	                                          "corrupted": { "option": "true" },
//	                                          "identified": { "option": "false" } } },
//	      "map_filters":       { "filters": { "map_tier": { "min": 15, "max": 15 } } },
//	      "equipment_filters": { "filters": { "rune_sockets": { "min": 2 } } }
//	    }
//	  },
//	  "sort": { "price": "asc" }
//	}
//
// Boolean options are the strings "true"/"false"; empty filter groups are omitted;
// disabled range filters are omitted while disabled stats are sent with `"disabled": true`
// so the trade site shows them unticked. The [RouteExchange] route uses a different
// endpoint body (`have`/`want` static ids) built by the trade package; this method always
// returns the search shape.
func (f *FilterSet) ToTradeQuery() TradeQuery {
	var status string
	switch f.Status {
	case StatusSecurable:
		status = "securable"
	case StatusOnline:
		status = "online"
	default:
		status = "any"
	}
	query := map[string]any{}
	query["status"] = map[string]any{"option": status}
	if f.Name != nil {
		query["name"] = *f.Name
	}
	if f.BaseType != nil {
		query["type"] = *f.BaseType
	}
	statFilters := make([]any, 0, len(f.Stats))
	for _, s := range f.Stats {
		statFilters = append(statFilters, map[string]any{
			"id":       s.StatID,
			"value":    minMax(s.Min, s.Max),
			"disabled": !s.Enabled,
		})
	}
	query["stats"] = []any{map[string]any{"type": "and", "filters": statFilters}}

	filters := map[string]any{}
	m := &f.Misc
	var category, rarity groupEntry
	if f.Category != nil {
		category = groupEntry{"category", map[string]any{"option": *f.Category}, true}
	}
	if f.Rarity != nil {
		rarity = groupEntry{"rarity", map[string]any{"option": *f.Rarity}, true}
	}
	entry := func(key string, v any, ok bool) groupEntry { return groupEntry{key, v, ok} }
	optBool := func(key string, b *bool) groupEntry {
		if b == nil {
			return groupEntry{}
		}
		return groupEntry{key, boolOption(*b), true}
	}
	ilvl, ilvlOK := rangeJSON(&m.ItemLevel)
	quality, qualityOK := rangeJSON(&m.Quality)
	addGroup(filters, "type_filters", []groupEntry{
		category, rarity, entry("ilvl", ilvl, ilvlOK), entry("quality", quality, qualityOK),
	})
	gemLevel, gemLevelOK := rangeJSON(&m.GemLevel)
	addGroup(filters, "misc_filters", []groupEntry{
		entry("gem_level", gemLevel, gemLevelOK), optBool("corrupted", m.Corrupted), optBool("identified", m.Identified),
	})
	tier, tierOK := rangeJSON(&m.WaystoneTier)
	addGroup(filters, "map_filters", []groupEntry{entry("map_tier", tier, tierOK)})
	sockets, socketsOK := rangeJSON(&m.Sockets)
	addGroup(filters, "equipment_filters", []groupEntry{entry("rune_sockets", sockets, socketsOK)})
	query["filters"] = filters

	return TradeQuery{Query: query, Sort: map[string]any{"price": "asc"}}
}

// clampF64 is `f64::clamp` (NaN passes through).
func clampF64(v, lo, hi float64) float64 {
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

// minF64 is `f64::min`: ignores NaN operands.
func minF64(a, b float64) float64 {
	switch {
	case math.IsNaN(a):
		return b
	case math.IsNaN(b):
		return a
	case b < a:
		return b
	}
	return a
}
