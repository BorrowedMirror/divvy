package trade

import (
	"context"

	"github.com/BorrowedMirror/divvy/internal/core"
)

// PriceRequest is the input to a price lookup.
type PriceRequest struct {
	Item    core.ParsedItem
	Filters core.FilterSet
}

// PriceSource is anything that can price an item. Implementations are safe for concurrent
// use.
type PriceSource interface {
	Kind() core.PriceSourceKind
	// Supports says whether this source can price the item at all (e.g. ninja:
	// currency/uniques only).
	Supports(item *core.ParsedItem) bool
	// Price prices the item. Errors are *Error, or ctx.Err() when ctx ends first.
	Price(ctx context.Context, req *PriceRequest) (core.PriceSummary, error)
}
