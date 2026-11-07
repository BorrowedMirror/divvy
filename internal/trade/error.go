package trade

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// ErrorKind is the variant of an [Error].
type ErrorKind int

// ErrorKind values (Rust `TradeError` variants).
const (
	// KindRateLimited: rate limited; RetryAfter says for how long.
	KindRateLimited ErrorKind = iota + 1
	// KindHTTP: a non-success status; Status and Message are set.
	KindHTTP
	// KindTransport: the request could not be sent or the body not read; Err is set.
	KindTransport
	// KindDecode: the response was not the expected JSON; Message is set.
	KindDecode
	// KindUnsupported: the price source cannot price the item; Source names it.
	KindUnsupported
	// KindConfig: invalid configuration; Message is set.
	KindConfig
)

// Error is a trade / price-source error (Rust `TradeError`).
type Error struct {
	Kind       ErrorKind
	RetryAfter time.Duration
	Status     uint16
	Message    string
	Source     string
	Err        error
}

// RateLimitedError is `TradeError::RateLimited`.
func RateLimitedError(retryAfter time.Duration) *Error {
	return &Error{Kind: KindRateLimited, RetryAfter: retryAfter}
}

// HTTPError is `TradeError::Http`.
func HTTPError(status uint16, message string) *Error {
	return &Error{Kind: KindHTTP, Status: status, Message: message}
}

// TransportError is `TradeError::Transport`.
func TransportError(err error) *Error { return &Error{Kind: KindTransport, Err: err} }

// DecodeError is `TradeError::Decode`.
func DecodeError(message string) *Error { return &Error{Kind: KindDecode, Message: message} }

// UnsupportedError is `TradeError::Unsupported`.
func UnsupportedError(source string) *Error { return &Error{Kind: KindUnsupported, Source: source} }

// ConfigError is `TradeError::Config`.
func ConfigError(message string) *Error { return &Error{Kind: KindConfig, Message: message} }

func (e *Error) Error() string {
	switch e.Kind {
	case KindRateLimited:
		return "rate limited; retry after " + rustDurationDebug(e.RetryAfter)
	case KindHTTP:
		return fmt.Sprintf("http %d: %s", e.Status, e.Message)
	case KindTransport:
		msg := "<nil>"
		if e.Err != nil {
			msg = e.Err.Error()
		}
		return "transport error: " + msg
	case KindDecode:
		return "invalid response: " + e.Message
	case KindUnsupported:
		return "item not supported by price source `" + e.Source + "`"
	case KindConfig:
		return "invalid configuration: " + e.Message
	}
	return "unknown trade error"
}

// Unwrap returns the transport cause.
func (e *Error) Unwrap() error { return e.Err }

// ErrorInfo is the IPC-friendly flattened error (Rust `TradeErrorInfo`).
type ErrorInfo struct {
	Kind         string  `json:"kind"`
	Message      string  `json:"message"`
	RetryAfterMs *uint64 `json:"retry_after_ms"`
}

// MarshalJSON writes serde-compatible JSON.
func (i ErrorInfo) MarshalJSON() ([]byte, error) {
	type plain ErrorInfo
	return core.MarshalSerde(plain(i))
}

// Info flattens the error.
func (e *Error) Info() ErrorInfo {
	var kind string
	var retry *uint64
	switch e.Kind {
	case KindRateLimited:
		kind = "rate_limited"
		ms := uint64(max(e.RetryAfter, 0).Milliseconds())
		retry = &ms
	case KindHTTP:
		kind = "http"
	case KindTransport:
		kind = "transport"
	case KindDecode:
		kind = "decode"
	case KindUnsupported:
		kind = "unsupported"
	case KindConfig:
		kind = "config"
	}
	return ErrorInfo{Kind: kind, Message: e.Error(), RetryAfterMs: retry}
}

// rustDurationDebug formats d like Rust's `{:?}` for std::time::Duration (`1.5s`, `60s`,
// `250ms`, `1.000001ms`, `0ns`).
func rustDurationDebug(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	ns := uint64(d)
	secs, nanos := ns/1e9, ns%1e9
	switch {
	case secs > 0:
		return decimal(secs, nanos, 9) + "s"
	case nanos >= 1_000_000:
		return decimal(nanos/1_000_000, nanos%1_000_000, 6) + "ms"
	case nanos >= 1_000:
		return decimal(nanos/1_000, nanos%1_000, 3) + "µs"
	default:
		return strconv.FormatUint(nanos, 10) + "ns"
	}
}

// decimal prints `int.frac` with frac zero-padded to digits and trailing zeros trimmed.
func decimal(integer, frac uint64, digits int) string {
	s := strconv.FormatUint(integer, 10)
	if frac == 0 {
		return s
	}
	f := strconv.FormatUint(frac, 10)
	f = strings.Repeat("0", digits-len(f)) + f
	return s + "." + strings.TrimRight(f, "0")
}
