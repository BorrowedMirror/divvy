package core

// The trade2 search body built from a [FilterSet].

// TradeQuery is the body sent to `/api/trade2/search/{league}`.
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
// so the trade site shows them unticked.
type TradeQuery struct {
	Query SearchQuery `json:"query"`
	Sort  SearchSort  `json:"sort"`
}

// SearchQuery is the `query` object of a [TradeQuery].
type SearchQuery struct {
	Status  Option        `json:"status"`
	Name    *string       `json:"name,omitzero"`
	Type    *string       `json:"type,omitzero"`
	Stats   []StatGroup   `json:"stats"`
	Filters SearchFilters `json:"filters"`
}

// SearchSort is the `sort` object of a [TradeQuery].
type SearchSort struct {
	Price string `json:"price"`
}

// Option is a trade2 option filter: `{"option": "..."}`.
type Option struct {
	Option string `json:"option"`
}

// Bounds is a trade2 range: `{"min": 1, "max": 2}`, either side optional.
type Bounds struct {
	Min *float64 `json:"min,omitzero"`
	Max *float64 `json:"max,omitzero"`
}

// StatGroup is one `stats` entry: a group of stat filters combined by Type (`and`).
type StatGroup struct {
	Type    string          `json:"type"`
	Filters []StatCondition `json:"filters"`
}

// StatCondition is one stat filter of a [StatGroup].
type StatCondition struct {
	ID       string `json:"id"`
	Value    Bounds `json:"value"`
	Disabled bool   `json:"disabled"`
}

// SearchFilters are the property filter groups of a search; a group without filters is
// left out.
type SearchFilters struct {
	Type      FilterGroup[TypeFilters]      `json:"type_filters,omitzero"`
	Misc      FilterGroup[MiscQuery]        `json:"misc_filters,omitzero"`
	Map       FilterGroup[MapFilters]       `json:"map_filters,omitzero"`
	Equipment FilterGroup[EquipmentFilters] `json:"equipment_filters,omitzero"`
}

// FilterGroup wraps a group's filters: `{"filters": {...}}`.
type FilterGroup[T any] struct {
	Filters T `json:"filters"`
}

// TypeFilters is the `type_filters` group.
type TypeFilters struct {
	Category  *Option `json:"category,omitzero"`
	Rarity    *Option `json:"rarity,omitzero"`
	ItemLevel *Bounds `json:"ilvl,omitzero"`
	Quality   *Bounds `json:"quality,omitzero"`
}

// MiscQuery is the `misc_filters` group.
type MiscQuery struct {
	GemLevel   *Bounds `json:"gem_level,omitzero"`
	Corrupted  *Option `json:"corrupted,omitzero"`
	Identified *Option `json:"identified,omitzero"`
}

// MapFilters is the `map_filters` group.
type MapFilters struct {
	MapTier *Bounds `json:"map_tier,omitzero"`
}

// EquipmentFilters is the `equipment_filters` group.
type EquipmentFilters struct {
	RuneSockets *Bounds `json:"rune_sockets,omitzero"`
}

// bounds is the range of an enabled filter with at least one bound, nil otherwise.
func (r *RangeFilter) bounds() *Bounds {
	if !r.Enabled || (r.Min == nil && r.Max == nil) {
		return nil
	}
	return &Bounds{Min: r.Min, Max: r.Max}
}

func boolOption(b *bool) *Option {
	switch {
	case b == nil:
		return nil
	case *b:
		return &Option{"true"}
	default:
		return &Option{"false"}
	}
}

func stringOption(s *string) *Option {
	if s == nil {
		return nil
	}
	return &Option{*s}
}

// ToTradeQuery builds the trade2 search body. The [RouteExchange] route uses a different
// endpoint body (`have`/`want` static ids) built by the trade package; this method always
// returns the search shape.
func (f *FilterSet) ToTradeQuery() TradeQuery {
	status := StatusAny
	if f.Status == StatusSecurable || f.Status == StatusOnline {
		status = f.Status
	}
	conditions := make([]StatCondition, 0, len(f.Stats))
	for _, s := range f.Stats {
		conditions = append(conditions, StatCondition{
			ID:       s.StatID,
			Value:    Bounds{Min: s.Min, Max: s.Max},
			Disabled: !s.Enabled,
		})
	}
	m := &f.Misc
	return TradeQuery{
		Query: SearchQuery{
			Status: Option{string(status)},
			Name:   f.Name,
			Type:   f.BaseType,
			Stats:  []StatGroup{{Type: "and", Filters: conditions}},
			Filters: SearchFilters{
				Type: FilterGroup[TypeFilters]{TypeFilters{
					Category:  stringOption(f.Category),
					Rarity:    stringOption(f.Rarity),
					ItemLevel: m.ItemLevel.bounds(),
					Quality:   m.Quality.bounds(),
				}},
				Misc: FilterGroup[MiscQuery]{MiscQuery{
					GemLevel:   m.GemLevel.bounds(),
					Corrupted:  boolOption(m.Corrupted),
					Identified: boolOption(m.Identified),
				}},
				Map:       FilterGroup[MapFilters]{MapFilters{MapTier: m.WaystoneTier.bounds()}},
				Equipment: FilterGroup[EquipmentFilters]{EquipmentFilters{RuneSockets: m.Sockets.bounds()}},
			},
		},
		Sort: SearchSort{Price: "asc"},
	}
}
