// Package ipc is the boundary between the Go backend and the React frontend: the bound Wails
// service (one method per command in web/src/bindings.ts), the event payloads, and the error
// shape every command returns. The behaviour lives in internal/app; this package only defines
// the wire contract, which `go run ./cmd/bindgen` turns into bindings.ts.
package ipc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

// IpcError is the error shape for every command.
type IpcError struct {
	// `parse`, `trade`, `rate_limited`, `config`, `platform`, `no_item`, `busy`, `update`, `internal`.
	Kind         string  `json:"kind"`
	Message      string  `json:"message"`
	RetryAfterMs *uint64 `json:"retry_after_ms"`
}

// NewError returns an IpcError without a retry delay.
func NewError(kind, message string) *IpcError {
	return &IpcError{Kind: kind, Message: message}
}

// Errorf returns an IpcError with a formatted message.
func Errorf(kind, format string, args ...any) *IpcError {
	return &IpcError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

func (e *IpcError) Error() string { return e.Kind + ": " + e.Message }

// MarshalJSON writes the error as serde did. Wails marshals a command's error with it, and the
// frontend receives it as the `error` of `{status: "error"}`.
func (e IpcError) MarshalJSON() ([]byte, error) {
	type plain IpcError
	return core.MarshalSerde(plain(e))
}

// UnmarshalJSON reads the serde shape.
func (e *IpcError) UnmarshalJSON(b []byte) error {
	type plain IpcError
	return json.Unmarshal(b, (*plain)(e))
}

// From converts any backend error into an IpcError, the way the Rust `From` impls did: trade
// errors keep their kind (`rate_limited` with its retry delay, otherwise `trade`), config and
// platform errors get their own kinds, and anything else is `internal`.
func From(err error) *IpcError {
	if err == nil {
		return nil
	}
	if e, ok := errors.AsType[*IpcError](err); ok {
		return e
	}
	// App-level errors name their IPC kind themselves (`parse` for item text, `platform` for a
	// permission request).
	if e, ok := errors.AsType[interface {
		error
		IPCKind() string
	}](err); ok {
		return NewError(e.IPCKind(), err.Error())
	}
	if e, ok := errors.AsType[*trade.Error](err); ok {
		info := e.Info()
		kind := "trade"
		if info.Kind == "rate_limited" {
			kind = "rate_limited"
		}
		return &IpcError{Kind: kind, Message: info.Message, RetryAfterMs: info.RetryAfterMs}
	}
	if _, ok := errors.AsType[*platform.Error](err); ok {
		return NewError("platform", err.Error())
	}
	if isConfigError(err) {
		return NewError("config", err.Error())
	}
	return NewError("internal", err.Error())
}

func isConfigError(err error) bool {
	_, io := errors.AsType[*config.IoError](err)
	_, parse := errors.AsType[*config.ParseError](err)
	_, ser := errors.AsType[*config.SerialiseError](err)
	_, tooNew := errors.AsType[*config.TooNewError](err)
	_, invalid := errors.AsType[*config.InvalidError](err)
	return io || parse || ser || tooNew || invalid || errors.Is(err, config.ErrNoConfigDir)
}

// Diagnostics is what the diagnostics page shows.
type Diagnostics struct {
	Version      string                `json:"version"`
	OS           string                `json:"os"`
	Backend      platform.BackendKind  `json:"backend"`
	Capabilities platform.Capabilities `json:"capabilities"`
	DataVersion  *string               `json:"data_version"`
	League       *string               `json:"league"`
	SettingsPath *string               `json:"settings_path"`
	LogPath      *string               `json:"log_path"`
	RecentErrors []string              `json:"recent_errors"`
}

// MarshalJSON writes serde's shape (an empty error list is `[]`).
func (d Diagnostics) MarshalJSON() ([]byte, error) {
	type plain Diagnostics
	return core.MarshalSerde(plain(d))
}

// PriceCheckRequest asks for a price check.
type PriceCheckRequest struct {
	// Item text to parse directly (debug / paste). `None` = run the copy flow against the game.
	Text   *string `json:"text"`
	Pinned bool    `json:"pinned"`
}

// PriceCheckResponse is the parsed item and its default filters.
type PriceCheckResponse struct {
	Item    core.ParsedItem `json:"item"`
	Filters core.FilterSet  `json:"filters"`
}

// MarshalJSON writes serde's shape.
func (r PriceCheckResponse) MarshalJSON() ([]byte, error) {
	type plain PriceCheckResponse
	return core.MarshalSerde(plain(r))
}

// DragPhase is the phase of a header drag of the price card.
type DragPhase string

const (
	DragStart DragPhase = "start"
	DragMove  DragPhase = "move"
	DragEnd   DragPhase = "end"
)

// UnmarshalJSON accepts only the three phases.
func (p *DragPhase) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch v := DragPhase(s); v {
	case DragStart, DragMove, DragEnd:
		*p = v
		return nil
	}
	return fmt.Errorf("unknown variant %q, expected one of `start`, `move`, `end`", s)
}

// --- events ---

// ItemCopied is sent when an item was copied from the game and parsed: the overlay shows it
// and starts querying.
type ItemCopied struct {
	Item    core.ParsedItem `json:"item"`
	Filters core.FilterSet  `json:"filters"`
	Pinned  bool            `json:"pinned"`
}

// MarshalJSON writes serde's shape.
func (e ItemCopied) MarshalJSON() ([]byte, error) {
	type plain ItemCopied
	return core.MarshalSerde(plain(e))
}

// PriceReady carries a finished price summary.
type PriceReady struct {
	Summary core.PriceSummary `json:"summary"`
}

// MarshalJSON writes serde's shape.
func (e PriceReady) MarshalJSON() ([]byte, error) {
	type plain PriceReady
	return core.MarshalSerde(plain(e))
}

// PriceFailed carries the error of a failed price query.
type PriceFailed struct {
	Error IpcError `json:"error"`
}

// SettingsChanged carries the settings after any change.
type SettingsChanged struct {
	Settings config.Settings `json:"settings"`
}

// MarshalJSON writes serde's shape.
func (e SettingsChanged) MarshalJSON() ([]byte, error) {
	type plain SettingsChanged
	return core.MarshalSerde(plain(e))
}

// OverlayState says whether the price panel is visible and pinned.
type OverlayState struct {
	Visible bool `json:"visible"`
	Pinned  bool `json:"pinned"`
}

// OverlayLayout is how the price panel window is laid out. Cover: the window covers the game
// with a dimmed backdrop and the card is drawn at (X, Y) (logical px inside the window);
// otherwise the window is the card itself (pinned, or no tracked game window).
type OverlayLayout struct {
	Cover bool    `json:"cover"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// MarshalJSON writes serde's shape (floats as serde_json formats them).
func (l OverlayLayout) MarshalJSON() ([]byte, error) {
	type plain OverlayLayout
	return core.MarshalSerde(plain(l))
}

// UpdateAvailable announces an update the user can install.
type UpdateAvailable struct {
	Version string `json:"version"`
	// Release notes from the updater manifest.
	Notes *string `json:"notes"`
	// Publish date (RFC 3339) as announced by the manifest.
	Date *string `json:"date"`
}

// Wire names of the events, as the frontend listens for them.
const (
	EventItemCopied         = "item-copied"
	EventOverlayLayout      = "overlay-layout"
	EventOverlayState       = "overlay-state"
	EventPermissionsChanged = "permissions-changed"
	EventPriceFailed        = "price-failed"
	EventPriceReady         = "price-ready"
	EventSettingsChanged    = "settings-changed"
	EventUpdateAvailable    = "update-available"
)
