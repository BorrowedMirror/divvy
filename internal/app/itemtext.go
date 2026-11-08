package app

import (
	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
)

// maxItemText is the largest clipboard text accepted as an item (real items are a few KiB).
const maxItemText = 64 * 1024

// itemTextError is returned when copied text is not an item. The IPC layer reports it with
// error kind `parse` and the message alone.
type itemTextError struct {
	msg string
	err error // the parser's error, nil for oversized text
}

func (e *itemTextError) Error() string { return e.msg }

func (e *itemTextError) Unwrap() error { return e.err }

var _ ipc.KindedError = (*itemTextError)(nil)

// IPCKind is the frontend error kind: [ipc.KindParse].
func (*itemTextError) IPCKind() ipc.Kind { return ipc.KindParse }

// uniqueIndex is a catalog that indexes uniques by base type.
type uniqueIndex interface {
	UniquesByBase(baseType string) []*core.UniqueDef
}

// processItemText parses clipboard text, matches stats and builds default filters. catalog may
// be nil (no stat matching, no unique candidates).
func processItemText(text string, catalog core.Catalog, opts *core.QueryOptions) (*core.ParsedItem, core.FilterSet, error) {
	if len(text) > maxItemText {
		return nil, core.FilterSet{}, &itemTextError{msg: "item text too large"}
	}
	item, err := core.ParseItem(text)
	if err != nil {
		return nil, core.FilterSet{}, &itemTextError{msg: err.Error(), err: err}
	}
	if catalog != nil {
		core.MatchStats(item, catalog)
	}
	filters := core.BuildFilters(item, opts)
	// Catalogs that index uniques by base list the candidates of an unidentified unique.
	if byBase, ok := catalog.(uniqueIndex); ok && item.Rarity == core.RarityUnique && item.Unidentified {
		uniques := byBase.UniquesByBase(item.BaseType)
		names := make([]string, 0, len(uniques))
		for _, u := range uniques {
			names = append(names, u.Name)
		}
		filters.UniqueCandidates = names
	}
	return item, filters, nil
}

// queryOptions derives query options from settings and the resolved league.
func queryOptions(settings *config.Settings, league string) core.QueryOptions {
	return core.QueryOptions{
		League:         league,
		Status:         settings.Trade.Sellers,
		WidenPct:       settings.Trade.WidenPct,
		EnableAllStats: settings.Trade.EnableAllStats,
	}
}
