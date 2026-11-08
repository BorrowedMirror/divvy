package core

// Game-data model consumed by the matcher. Implemented by internal/data; faked in tests.

// StatDef is one trade stat as exposed by `/api/trade2/data/stats`.
type StatDef struct {
	// e.g. `explicit.stat_3299347043`
	ID string `json:"id"`
	// Template text with `#` placeholders, e.g. `+# to maximum Life`.
	Text string `json:"text"`
	// `explicit`, `implicit`, `rune`, `enchant`, `pseudo`, ...
	Kind string `json:"kind"`
	// Alternative templates that map to the same stat (e.g. "reduced" vs "increased").
	// May be missing when decoding.
	Aliases []StatAlias `json:"aliases"`
}

// StatAlias is an alternative wording; Negate means the values must be multiplied by -1.
type StatAlias struct {
	Text   string `json:"text"`
	Negate bool   `json:"negate"`
}

// BaseDef is an item base as exposed by `/api/trade2/data/items` plus export data.
type BaseDef struct {
	Name string `json:"name"`
	// Trade category, e.g. `armour.helmet`.
	Category  string `json:"category"`
	ItemClass string `json:"item_class"`
}

// UniqueDef is a unique item and its base.
type UniqueDef struct {
	Name     string `json:"name"`
	BaseType string `json:"base_type"`
}

// StaticDef is a static currency / exchange-able item (`/api/trade2/data/static`).
type StaticDef struct {
	// Exchange id, e.g. `divine`.
	ID    string  `json:"id"`
	Text  string  `json:"text"`
	Group string  `json:"group"`
	Image *string `json:"image"`
}

// StatMatch is one result of [Catalog.StatsByTemplate]: the stat, and whether the
// template was an alias whose values must be negated.
type StatMatch struct {
	Stat   *StatDef
	Negate bool
}

// Catalog is a read-only view over bundled game data. Implementations must be safe for
// concurrent use.
type Catalog interface {
	// StatsByTemplate returns the stats whose template (or alias) equals template for the
	// given modifier kind.
	StatsByTemplate(template string, kind ModKind) []StatMatch
	// Base looks a base up by exact name (nil when unknown).
	Base(name string) *BaseDef
	// BaseInMagicName finds the base contained in a magic item's full name
	// (`Prefix Base of Suffix`).
	BaseInMagicName(fullName string) *BaseDef
	// Unique looks a unique up by name.
	Unique(name string) *UniqueDef
	// StaticItem looks a static (exchange) item up by its text.
	StaticItem(text string) *StaticDef
}
