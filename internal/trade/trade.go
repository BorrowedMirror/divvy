// Package trade is RKL's network side, ported from the Rust crate rkl-trade: the official
// PoE2 trade2 API ([TradeClient]), poe.ninja ([PoeNinjaSource]) and poe2scout
// ([Poe2ScoutSource]) behind [PriceSource], with a header-driven [RateLimiter] and an otter
// response cache.
//
// Base URLs are plain configuration here; the desktop app reads the RKL_TRADE_BASE_URL,
// RKL_NINJA_BASE_URL and RKL_SCOUT_BASE_URL overrides and passes them in.
package trade

// Version is the application version reported in the default User-Agent (the Rust crate used
// CARGO_PKG_VERSION). Override at link time with
// `-ldflags "-X github.com/BorrowedMirror/rust-king-ledger/internal/trade.Version=..."`.
var Version = "dev"

// DefaultUserAgent is the default User-Agent. GGG asks for a descriptive UA with contact
// details; users can append their own contact e-mail via settings (`trade.contact`).
func DefaultUserAgent() string {
	return "rkl/" + Version +
		" (+https://github.com/BorrowedMirror/rust-king-ledger; contact: https://github.com/BorrowedMirror/rust-king-ledger/issues)"
}
