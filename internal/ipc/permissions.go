package ipc

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// PermissionInfo is one OS permission and whether it is currently granted.
type PermissionInfo struct {
	// Kind is `accessibility`, `screen_recording` or `input_monitoring`.
	Kind    string `json:"kind"`
	Granted bool   `json:"granted"`
	// Required means the core price-check flow does not work without it.
	Required bool `json:"required"`
	// SettingsURL is the System Settings deep link, if the OS has one.
	SettingsURL *string `json:"settings_url"`
}

// Equal compares permissions by value (settings URLs by their text).
func (p PermissionInfo) Equal(o PermissionInfo) bool {
	return p.Kind == o.Kind && p.Granted == o.Granted && p.Required == o.Required &&
		(p.SettingsURL == nil) == (o.SettingsURL == nil) &&
		(p.SettingsURL == nil || *p.SettingsURL == *o.SettingsURL)
}

// PermissionReport is what the onboarding view shows.
type PermissionReport struct {
	Platform platform.BackendKind `json:"platform"`
	// Missing is every permission relevant on this platform (granted ones included, so the UI
	// can tick them off live). Empty where the OS has no permission model (Windows, Linux).
	Missing []PermissionInfo `json:"missing"`
	// Notes are degradation notes (English, for the details list / bug reports).
	Notes []string `json:"notes"`
	// GNOME is set when running under GNOME (`XDG_CURRENT_DESKTOP`), which only supports
	// X11/XWayland.
	GNOME bool `json:"gnome"`
}

// Blocked reports whether a required permission is still missing.
func (r PermissionReport) Blocked() bool {
	return slices.ContainsFunc(r.Missing, func(p PermissionInfo) bool { return p.Required && !p.Granted })
}

// Equal compares reports by value (nil and empty lists are equal).
func (r PermissionReport) Equal(o PermissionReport) bool {
	return r.Platform == o.Platform && r.GNOME == o.GNOME && slices.Equal(r.Notes, o.Notes) &&
		slices.EqualFunc(r.Missing, o.Missing, PermissionInfo.Equal)
}

// MarshalJSONTo writes the wire format.
func (r PermissionReport) MarshalJSONTo(enc *jsontext.Encoder) error {
	type plain PermissionReport
	return marshalWire(enc, plain(r))
}
