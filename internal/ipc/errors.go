package ipc

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

// Kind is what went wrong, as the frontend tells errors apart (`IpcError.kind`).
type Kind string

// Error kinds.
const (
	// KindParse is item text that does not parse.
	KindParse Kind = "parse"
	// KindTrade is a failed trade site request.
	KindTrade Kind = "trade"
	// KindRateLimited is a trade site rate limit; the error says when to retry.
	KindRateLimited Kind = "rate_limited"
	// KindConfig is a settings problem (loading, saving, an invalid value).
	KindConfig Kind = "config"
	// KindPlatform is an OS integration failure (no backend, a missing permission).
	KindPlatform Kind = "platform"
	// KindNoItem is a price check without an item (none copied, the game not focused).
	KindNoItem Kind = "no_item"
	// KindBusy is a request dropped because another price check is running.
	KindBusy Kind = "busy"
	// KindUpdate is a failed or unavailable app update.
	KindUpdate Kind = "update"
	// KindInternal is anything else.
	KindInternal Kind = "internal"
)

// Error is the error every command fails with, which the frontend receives as the `error` of
// `{status: "error"}`.
//
//nolint:recvcheck // MarshalJSONTo has a value receiver so an Error inside a payload (PriceFailed) marshals too.
type Error struct {
	// `parse`, `trade`, `rate_limited`, `config`, `platform`, `no_item`, `busy`, `update`, `internal`.
	Kind         Kind    `json:"kind" ts:"type=string"`
	Message      string  `json:"message"`
	RetryAfterMS *uint64 `json:"retry_after_ms"`
	// Err is the cause, for errors.Is and errors.As in Go; it is not sent.
	Err error `json:"-"`
}

// NewError returns an Error without a retry delay.
func NewError(kind Kind, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

// Errorf returns an Error with a formatted message. With a %w verb, the operand is the cause.
func Errorf(kind Kind, format string, args ...any) *Error {
	err := fmt.Errorf(format, args...)
	e := &Error{Kind: kind, Message: err.Error()}
	if strings.Contains(format, "%w") {
		e.Err = err
	}
	return e
}

func (e *Error) Error() string { return string(e.Kind) + ": " + e.Message }

// Unwrap returns the cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// MarshalJSONTo writes the error in the wire format. Wails marshals a command's error with
// it.
func (e Error) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain Error
	return marshalWire(enc, plain(e))
}

// KindedError is an error that names its own kind (`parse` for item text that is not an
// item, `platform` for a permission request), which [From] keeps.
type KindedError interface {
	error
	IPCKind() Kind
}

// From converts any backend error into an Error: trade errors keep their kind
// (`rate_limited` with its retry delay, otherwise `trade`), config and platform errors get
// their own kinds, and anything else is `internal`. The original error is the cause.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := errors.AsType[*Error](err); ok {
		return e
	}
	if e, ok := errors.AsType[KindedError](err); ok {
		return &Error{Kind: e.IPCKind(), Message: err.Error(), Err: err}
	}
	if e, ok := errors.AsType[*trade.Error](err); ok {
		info := e.Info()
		kind := KindTrade
		if e.Kind == trade.KindRateLimited {
			kind = KindRateLimited
		}
		return &Error{Kind: kind, Message: info.Message, RetryAfterMS: info.RetryAfterMS, Err: err}
	}
	kind := KindInternal
	if _, ok := errors.AsType[*platform.Error](err); ok {
		kind = KindPlatform
	} else if isConfigError(err) {
		kind = KindConfig
	}
	return &Error{Kind: kind, Message: err.Error(), Err: err}
}

func isConfigError(err error) bool {
	_, io := errors.AsType[*config.IOError](err)
	_, parse := errors.AsType[*config.ParseError](err)
	_, ser := errors.AsType[*config.SerialiseError](err)
	_, tooNew := errors.AsType[*config.TooNewError](err)
	_, invalid := errors.AsType[*config.InvalidError](err)
	return io || parse || ser || tooNew || invalid || errors.Is(err, config.ErrNoConfigDir)
}
