package trade

import (
	"fmt"
	"time"
)

// ErrorKind classifies an [Error].
type ErrorKind int

// ErrorKind values.
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

// Error is a trade / price-source error.
type Error struct {
	Kind       ErrorKind
	RetryAfter time.Duration
	Status     int
	Message    string
	Source     string
	Err        error
}

func rateLimitedError(retryAfter time.Duration) *Error {
	return &Error{Kind: KindRateLimited, RetryAfter: retryAfter}
}

func httpError(status int, message string) *Error {
	return &Error{Kind: KindHTTP, Status: status, Message: message}
}

func transportError(err error) *Error { return &Error{Kind: KindTransport, Err: err} }

func decodeError(format string, args ...any) *Error {
	return &Error{Kind: KindDecode, Message: fmt.Sprintf(format, args...)}
}

func unsupportedError(source string) *Error { return &Error{Kind: KindUnsupported, Source: source} }

func configError(format string, args ...any) *Error {
	return &Error{Kind: KindConfig, Message: fmt.Sprintf(format, args...)}
}

func (e *Error) Error() string {
	switch e.Kind {
	case KindRateLimited:
		return "rate limited; retry after " + formatRetry(e.RetryAfter)
	case KindHTTP:
		return fmt.Sprintf("http %d: %s", e.Status, e.Message)
	case KindTransport:
		return fmt.Sprintf("transport error: %v", e.Err)
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

// formatRetry writes a retry delay for people: whole seconds (rounded up) from one second,
// milliseconds below.
func formatRetry(d time.Duration) string {
	if d < time.Second {
		return max(d, 0).Round(time.Millisecond).String()
	}
	return fmt.Sprintf("%ds", (d+time.Second-1)/time.Second)
}

// ErrorInfo is the error flattened for the frontend.
type ErrorInfo struct {
	Kind         string  `json:"kind"`
	Message      string  `json:"message"`
	RetryAfterMS *uint64 `json:"retry_after_ms"`
}

// Info flattens the error.
func (e *Error) Info() ErrorInfo {
	var kind string
	var retry *uint64
	switch e.Kind {
	case KindRateLimited:
		kind = "rate_limited"
		retry = new(uint64(max(e.RetryAfter, 0).Milliseconds()))
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
	return ErrorInfo{Kind: kind, Message: e.Error(), RetryAfterMS: retry}
}
