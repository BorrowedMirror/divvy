// Package core is RKL's pure domain logic: no I/O, no platform, no network.
//
// Pipeline: clipboard text --[ParseItem]--> [ParsedItem]
// --[MatchStats]--> matched stat ids
// --[BuildFilters]--> [FilterSet] (user-editable)
// --[FilterSet.ToTradeQuery]--> [TradeQuery] (trade2 search body)
// and listings --[Summarise]--> [PriceSummary].
//
// The types that reach the frontend carry `json` tags naming the keys of its TypeScript
// types (web/src/bindings.ts). String enums reject unknown values when decoded; how nil
// slices and maps are written on the wire is decided by internal/ipc.
package core
