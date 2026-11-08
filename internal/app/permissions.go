package app

// First-run permission report and platform degradation notes for the onboarding view.
//
// On macOS the report comes from live TCC checks (supplied by the platform backend as a
// [PermissionProbe]); on every other platform it is derived from the backend's
// [platform.Capabilities]. The web onboarding view polls [Service.RecheckPermissions] (every
// 2 s, only while that view is visible), and [ipc.PermissionsChanged] is emitted whenever the
// answer changes. At start-up, [NeedsOnboarding] decides whether settings open on
// `#/onboarding`.

import (
	"fmt"
	"slices"
	"sync"

	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Permission kinds, as [platform.Permission] names them.
const (
	permAccessibility   = string(platform.PermissionAccessibility)
	permScreenRecording = string(platform.PermissionScreenRecording)
	permInputMonitoring = string(platform.PermissionInputMonitoring)
)

// macosPermission is a macOS permission with its "required" flag.
type macosPermission struct {
	kind     platform.Permission
	required bool
}

// macosPermissions lists the macOS permissions in the order the onboarding shows them (see
// docs/research/macos.md).
var macosPermissions = [...]macosPermission{
	{platform.PermissionAccessibility, true},
	{platform.PermissionScreenRecording, true},
	{platform.PermissionInputMonitoring, false},
}

// Degradation notes.
const (
	noteWaylandPortal     = "Wayland-native: global hotkeys use the GlobalShortcuts desktop portal when the compositor offers it"
	noteWaylandTrigger    = "Wayland-native: without the portal, bind `rkl --trigger` (add `--pinned` for pinned) in your compositor"
	noteWaylandLayerShell = "Wayland-native: the overlay is anchored to the output with layer-shell, not to the game window"
	noteGNOME             = "GNOME: only X11/XWayland is supported; run the game under XWayland"
)

// PermissionProbe is the source of live permission state. On macOS the platform backend
// implements it with TCC checks; elsewhere the capability list is repeated.
type PermissionProbe interface {
	// Missing returns the permission kinds currently missing.
	Missing() []string
	// OpenSettings prompts for, or opens the settings pane of, kind.
	OpenSettings(kind string)
}

// staticProbe just repeats the backend's start-up capability report.
type staticProbe []string

func (p staticProbe) Missing() []string { return slices.Clone(p) }

func (staticProbe) OpenSettings(string) {}

// buildPermissionReport builds the report from the missing permission kinds and the
// capability notes.
func buildPermissionReport(kind platform.BackendKind, missing, capsNotes []string, gnome bool) ipc.PermissionReport {
	permissions := []ipc.PermissionInfo{}
	if kind == platform.BackendMacOS {
		for _, p := range macosPermissions {
			url, _ := p.kind.SettingsURL()
			permissions = append(permissions, ipc.PermissionInfo{
				Kind:        string(p.kind),
				Granted:     !slices.Contains(missing, string(p.kind)),
				Required:    p.required,
				SettingsURL: new(url),
			})
		}
	} else {
		for _, m := range missing {
			permissions = append(permissions, ipc.PermissionInfo{Kind: m})
		}
	}
	notes := []string{}
	if kind == platform.BackendWayland {
		notes = append(notes, noteWaylandPortal, noteWaylandTrigger, noteWaylandLayerShell)
	}
	if gnome {
		notes = append(notes, noteGNOME)
	}
	for _, n := range capsNotes {
		if !slices.Contains(notes, n) {
			notes = append(notes, n)
		}
	}
	return ipc.PermissionReport{Platform: kind, Missing: permissions, Notes: notes, GNOME: gnome}
}

// NeedsOnboarding reports whether the settings window should open on `#/onboarding` at
// start-up: on first run, or while a required permission is missing.
func NeedsOnboarding(onboardingDone bool, report ipc.PermissionReport) bool {
	return !onboardingDone || report.Blocked()
}

// permissionRequestError is returned by [Permissions.request] for a kind that is not a
// permission on this platform. The IPC layer reports it with error kind `platform`.
type permissionRequestError struct {
	permission string
}

func (e *permissionRequestError) Error() string {
	return fmt.Sprintf("permission `%s` cannot be requested on this platform", e.permission)
}

var _ ipc.KindedError = (*permissionRequestError)(nil)

// IPCKind is the frontend error kind: [ipc.KindPlatform].
func (*permissionRequestError) IPCKind() ipc.Kind { return ipc.KindPlatform }

// Permissions is the permission state: the probe plus the last report seen (for change
// events). Safe for concurrent use.
type Permissions struct {
	platform platform.BackendKind
	gnome    bool
	caps     platform.Capabilities
	probe    PermissionProbe

	mu   sync.Mutex
	last *ipc.PermissionReport // nil until the first report
}

func newPermissions(kind platform.BackendKind, caps platform.Capabilities, gnome bool, probe PermissionProbe) *Permissions {
	return &Permissions{platform: kind, gnome: gnome, caps: caps, probe: probe}
}

// PermissionsForBackend picks the probe for the running platform: live (the backend's TCC
// checks) on macOS when supplied, otherwise the capability report's missing permissions.
// gnome is [platform.EnvProbe.IsGNOME] of the environment.
func PermissionsForBackend(kind platform.BackendKind, caps platform.Capabilities, gnome bool, live PermissionProbe) *Permissions {
	var probe PermissionProbe = staticProbe(slices.Clone(caps.MissingPermissions))
	if kind == platform.BackendMacOS && live != nil {
		probe = live
	}
	return newPermissions(kind, caps, gnome, probe)
}

func (p *Permissions) report() ipc.PermissionReport {
	return buildPermissionReport(p.platform, p.probe.Missing(), p.caps.Notes, p.gnome)
}

// Current returns the current report and remembers it as the baseline for later rechecks.
func (p *Permissions) Current() ipc.PermissionReport {
	report := p.report()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = &report
	return report
}

// recheck re-queries the probe. It returns the report and whether it differs from the previous
// one (never on the first call, which only sets the baseline).
func (p *Permissions) recheck() (ipc.PermissionReport, bool) {
	report := p.report()
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := p.last != nil && !p.last.Equal(report)
	p.last = &report
	return report, changed
}

// request opens the settings pane for kind if it is a permission on this platform.
func (p *Permissions) request(kind string) error {
	known := p.platform == platform.BackendMacOS &&
		slices.ContainsFunc(macosPermissions[:], func(m macosPermission) bool { return string(m.kind) == kind })
	if !known {
		return &permissionRequestError{permission: kind}
	}
	p.probe.OpenSettings(kind)
	return nil
}
