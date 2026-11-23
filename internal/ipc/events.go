package ipc

import (
	"encoding/json/jsontext"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
)

// Event names on the bus, as the frontend listens for them. Each carries the payload type of
// the same name.
const (
	EventItemCopied         = "item-copied"
	EventOverlayLayout      = "overlay-layout"
	EventOverlayState       = "overlay-state"
	EventPermissionsChanged = "permissions-changed"
	EventPriceFailed        = "price-failed"
	EventPriceReady         = "price-ready"
	EventReadyNotice        = "ready-notice"
	EventSettingsChanged    = "settings-changed"
	EventUpdateAvailable    = "update-available"
)

// ItemCopied is sent when an item was copied from the game and parsed: the overlay shows it
// and starts querying.
type ItemCopied struct {
	Item    core.ParsedItem `json:"item"`
	Filters core.FilterSet  `json:"filters"`
	Pinned  bool            `json:"pinned"`
}

// MarshalJSONTo writes the wire format.
func (e ItemCopied) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain ItemCopied
	return marshalWire(enc, plain(e))
}

// PriceReady carries a finished price summary.
type PriceReady struct {
	Summary core.PriceSummary `json:"summary"`
}

// MarshalJSONTo writes the wire format.
func (e PriceReady) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain PriceReady
	return marshalWire(enc, plain(e))
}

// PriceFailed carries the error of a failed price query.
type PriceFailed struct {
	Error Error `json:"error"`
}

// MarshalJSONTo writes the wire format.
func (e PriceFailed) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain PriceFailed
	return marshalWire(enc, plain(e))
}

// SettingsChanged carries the settings after any change.
type SettingsChanged struct {
	Settings config.Settings `json:"settings"`
}

// MarshalJSONTo writes the wire format.
func (e SettingsChanged) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain SettingsChanged
	return marshalWire(enc, plain(e))
}

// OverlayState says whether the price panel is visible and pinned.
type OverlayState struct {
	Visible bool `json:"visible"`
	Pinned  bool `json:"pinned"`
}

// MarshalJSONTo writes the wire format.
func (s OverlayState) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain OverlayState
	return marshalWire(enc, plain(s))
}

// OverlayLayout is how the price panel window is laid out. Cover: the window covers the game
// with a dimmed backdrop and the card is drawn at (X, Y) (logical px inside the window);
// otherwise the window is the card itself (pinned, or no tracked game window).
type OverlayLayout struct {
	Cover bool    `json:"cover"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// MarshalJSONTo writes the wire format.
func (l OverlayLayout) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain OverlayLayout
	return marshalWire(enc, plain(l))
}

// UpdateAvailable announces an update the user can install.
type UpdateAvailable struct {
	Version string `json:"version"`
	// Release notes from the updater manifest.
	Notes *string `json:"notes"`
	// Publish date (RFC 3339) as announced by the manifest.
	Date *string `json:"date"`
}

// MarshalJSONTo writes the wire format.
func (u UpdateAvailable) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain UpdateAvailable
	return marshalWire(enc, plain(u))
}

// PermissionsChanged is pushed when a recheck finds the permission state changed.
type PermissionsChanged struct {
	Report PermissionReport `json:"report"`
}

// MarshalJSONTo writes the wire format.
func (e PermissionsChanged) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain PermissionsChanged
	return marshalWire(enc, plain(e))
}

// ReadyNotice shows the "ready" notice over a newly found game window for DurationMS.
type ReadyNotice struct {
	DurationMS uint32 `json:"duration_ms"`
}

// MarshalJSONTo writes the wire format.
func (e ReadyNotice) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain ReadyNotice
	return marshalWire(enc, plain(e))
}
