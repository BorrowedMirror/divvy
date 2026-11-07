package app

// First-run permission report and platform degradation notes for the onboarding view.
//
// On macOS the report comes from live TCC checks (supplied by the platform backend as a
// [PermissionProbe]); on every other platform it is derived from the backend's
// [platform.Capabilities]. The web onboarding view polls `recheck_permissions` (every 2 s, only
// while that view is visible) and [PermissionsChanged] is emitted whenever the answer changes.
//
// The IPC commands (`get_permissions`, `request_permission`, `recheck_permissions`) and the
// start-up hook that opens `#/onboarding` belong to the shell; they wrap [Permissions].

import (
	"fmt"
	"slices"
	"sync"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Permission kinds.
const (
	PermAccessibility   = "accessibility"
	PermScreenRecording = "screen_recording"
	PermInputMonitoring = "input_monitoring"
)

// macosPermission is a macOS permission with its "required" flag and System Settings deep link.
type macosPermission struct {
	kind     string
	required bool
	url      string
}

// macosPermissions lists the macOS permissions in the order the onboarding shows them (see
// docs/research/macos.md).
var macosPermissions = [...]macosPermission{
	{PermAccessibility, true, "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"},
	{PermScreenRecording, true, "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"},
	{PermInputMonitoring, false, "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"},
}

// Degradation notes.
const (
	NoteWaylandPortal     = "Wayland-native: global hotkeys use the GlobalShortcuts desktop portal when the compositor offers it"
	NoteWaylandTrigger    = "Wayland-native: without the portal, bind `rkl --trigger` (add `--pinned` for pinned) in your compositor"
	NoteWaylandLayerShell = "Wayland-native: the overlay is anchored to the output with layer-shell, not to the game window"
	NoteGNOME             = "GNOME: only X11/XWayland is supported; run the game under XWayland"
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

func (p PermissionInfo) equal(o PermissionInfo) bool {
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
		slices.EqualFunc(r.Missing, o.Missing, PermissionInfo.equal)
}

// PermissionsChanged is pushed (event `permissions-changed`) when a recheck finds the
// permission state changed.
type PermissionsChanged struct {
	Report PermissionReport `json:"report"`
}

// PermissionsChangedEvent is the frontend event name of [PermissionsChanged].
const PermissionsChangedEvent = "permissions-changed"

// PermissionProbe is the source of live permission state. On macOS the platform backend
// implements it with TCC checks (`recheck_permissions` / `open_permission_settings` in the Rust
// backend); elsewhere [StaticProbe] repeats the capability list.
type PermissionProbe interface {
	// Missing returns the permission kinds currently missing.
	Missing() []string
	// OpenSettings prompts for, or opens the settings pane of, kind.
	OpenSettings(kind string)
}

// StaticProbe just repeats the backend's start-up capability report.
type StaticProbe []string

// Missing returns the static list.
func (p StaticProbe) Missing() []string { return slices.Clone(p) }

// OpenSettings does nothing.
func (StaticProbe) OpenSettings(string) {}

// BuildPermissionReport is the pure report builder (unit-tested without a backend).
func BuildPermissionReport(kind platform.BackendKind, missing, capsNotes []string, gnome bool) PermissionReport {
	permissions := []PermissionInfo{}
	if kind == platform.BackendMacos {
		for _, p := range macosPermissions {
			permissions = append(permissions, PermissionInfo{
				Kind:        p.kind,
				Granted:     !slices.Contains(missing, p.kind),
				Required:    p.required,
				SettingsURL: new(p.url),
			})
		}
	} else {
		for _, m := range missing {
			permissions = append(permissions, PermissionInfo{Kind: m})
		}
	}
	notes := []string{}
	if kind == platform.BackendWayland {
		notes = append(notes, NoteWaylandPortal, NoteWaylandTrigger, NoteWaylandLayerShell)
	}
	if gnome {
		notes = append(notes, NoteGNOME)
	}
	for _, n := range capsNotes {
		if !slices.Contains(notes, n) {
			notes = append(notes, n)
		}
	}
	return PermissionReport{Platform: kind, Missing: permissions, Notes: notes, GNOME: gnome}
}

// NeedsOnboarding reports whether the settings window should open on `#/onboarding` at
// start-up.
func NeedsOnboarding(onboardingDone bool, report PermissionReport) bool {
	return !onboardingDone || report.Blocked()
}

// PermissionRequestError is returned by [Permissions.Request] for a kind that is not a
// permission on this platform. The IPC layer reports it with error kind [PermissionRequestError.IPCKind].
type PermissionRequestError struct {
	Permission string
}

func (e *PermissionRequestError) Error() string {
	return fmt.Sprintf("permission `%s` cannot be requested on this platform", e.Permission)
}

// IPCKind is the frontend error kind (`IpcError.kind`): `platform`.
func (*PermissionRequestError) IPCKind() string { return "platform" }

// Permissions is the shell's permission state: the probe plus the last report seen (for change
// events). Safe for concurrent use.
type Permissions struct {
	platform platform.BackendKind
	gnome    bool
	caps     platform.Capabilities
	probe    PermissionProbe

	mu   sync.Mutex
	last *PermissionReport
}

// NewPermissions builds the state from an explicit probe.
func NewPermissions(kind platform.BackendKind, caps platform.Capabilities, gnome bool, probe PermissionProbe) *Permissions {
	return &Permissions{platform: kind, gnome: gnome, caps: caps, probe: probe}
}

// PermissionsForBackend picks the probe for the running platform: live (the backend's TCC
// checks) on macOS when supplied, otherwise a [StaticProbe] of the capability report. gnome is
// [platform.EnvProbe.IsGNOME] of the environment.
func PermissionsForBackend(kind platform.BackendKind, caps platform.Capabilities, gnome bool, live PermissionProbe) *Permissions {
	var probe PermissionProbe = StaticProbe(slices.Clone(caps.MissingPermissions))
	if kind == platform.BackendMacos && live != nil {
		probe = live
	}
	return NewPermissions(kind, caps, gnome, probe)
}

// String identifies the state in logs.
func (p *Permissions) String() string {
	return fmt.Sprintf("Permissions{platform: %s, ..}", p.platform)
}

func (p *Permissions) report() PermissionReport {
	return BuildPermissionReport(p.platform, p.probe.Missing(), p.caps.Notes, p.gnome)
}

// Current returns the current report and remembers it as the baseline for [Permissions.Recheck].
func (p *Permissions) Current() PermissionReport {
	report := p.report()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = &report
	return report
}

// Recheck re-queries the probe. It returns the report and whether it differs from the previous
// one (never on the first call, which only sets the baseline).
func (p *Permissions) Recheck() (PermissionReport, bool) {
	report := p.report()
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := p.last != nil && !p.last.Equal(report)
	p.last = &report
	return report, changed
}

// Request opens the settings pane for kind if it is a permission on this platform.
func (p *Permissions) Request(kind string) error {
	known := p.platform == platform.BackendMacos &&
		slices.ContainsFunc(macosPermissions[:], func(m macosPermission) bool { return m.kind == kind })
	if !known {
		return &PermissionRequestError{Permission: kind}
	}
	p.probe.OpenSettings(kind)
	return nil
}
