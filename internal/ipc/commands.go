package ipc

import (
	"encoding/json/jsontext"

	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/enum"
	"github.com/BorrowedMirror/divvy/internal/platform"
)

// PriceCheckRequest asks for a price check.
type PriceCheckRequest struct {
	// Item text to parse directly (debug / paste). `null` = run the copy flow against the game.
	Text   *string `json:"text"`
	Pinned bool    `json:"pinned"`
}

// PriceCheckResponse is the parsed item and its default filters.
type PriceCheckResponse struct {
	Item    core.ParsedItem `json:"item"`
	Filters core.FilterSet  `json:"filters"`
}

// MarshalJSONTo writes the wire format.
func (r PriceCheckResponse) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain PriceCheckResponse
	return marshalWire(enc, plain(r))
}

// DragPhase is the phase of a header drag of the price card.
type DragPhase string

// Drag phases.
const (
	// DragStart is the press on the header that starts a drag.
	DragStart DragPhase = "start"
	// DragMove is a pointer move while dragging.
	DragMove DragPhase = "move"
	// DragEnd is the release that ends a drag.
	DragEnd DragPhase = "end"
)

// UnmarshalText accepts only the three phases.
func (p *DragPhase) UnmarshalText(text []byte) error {
	return enum.Parse(p, text, DragStart, DragMove, DragEnd)
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
	// GNOME is set when running under GNOME (`XDG_CURRENT_DESKTOP`), which only supports
	// X11/XWayland.
	GNOME bool `json:"gnome"`
}

// MarshalJSONTo writes the wire format.
func (d Diagnostics) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain Diagnostics
	return marshalWire(enc, plain(d))
}
