// Package trade is RKL's network side: the official PoE2 trade2 API ([Client]), poe.ninja
// ([NinjaSource]) and poe2scout ([ScoutSource]) behind [PriceSource], with a header-driven
// rate limiter (package ratelimit) and an otter response cache.
//
// Base URLs are plain configuration here; the desktop app reads the RKL_TRADE_BASE_URL,
// RKL_NINJA_BASE_URL and RKL_SCOUT_BASE_URL overrides and passes them in.
package trade

// DefaultUserAgent is the default User-Agent for the given application version. GGG asks
// for a descriptive UA with contact details; users can append their own contact e-mail via
// settings (`trade.contact`).
func DefaultUserAgent(version string) string {
	return "rkl/" + version +
		" (+https://github.com/BorrowedMirror/rust-king-ledger; contact: https://github.com/BorrowedMirror/rust-king-ledger/issues)"
}
