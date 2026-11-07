// Package core is RKL's pure domain logic, ported from the Rust crate rkl-core. No I/O, no
// platform, no network.
//
// Pipeline: clipboard text --[ParseItem]--> [ParsedItem]
// --[StatMatcher.MatchItem]--> matched stat ids
// --[BuildFilters]--> [FilterSet] (user-editable)
// --[FilterSet.ToTradeQuery]--> [TradeQuery] (trade2 JSON body)
// and listings --[Summarise]--> [PriceSummary].
//
// Every type that crosses to the frontend encodes byte-identically to the Rust serde
// derives (see [MarshalSerde]).
package core
