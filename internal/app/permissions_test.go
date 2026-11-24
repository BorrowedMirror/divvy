package app

import (
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/ipc"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/platform/platformtest"
)

// fakeProbe is a fake TCC: missing kinds can be changed between rechecks; records opened panes.
type fakeProbe struct {
	mu      sync.Mutex
	missing []string
	opened  []string
	calls   int
}

func (f *fakeProbe) Missing() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return slices.Clone(f.missing)
}

func (f *fakeProbe) OpenSettings(kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, kind)
}

func (f *fakeProbe) setMissing(m ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.missing = m
}

func TestPermissionsMacosListsAllPermissionsInOrderWithDeepLinks(t *testing.T) {
	r := buildPermissionReport(platform.BackendMacOS, []string{permScreenRecording}, nil, false)
	type row struct {
		kind              string
		granted, required bool
	}
	var got []row
	for _, p := range r.Missing {
		got = append(got, row{p.Kind, p.Granted, p.Required})
		require.NotNil(t, p.SettingsURL)
		assert.Regexp(t, "^x-apple.systempreferences:", *p.SettingsURL)
	}
	assert.Equal(t, []row{{permAccessibility, true, true}, {permScreenRecording, false, true}, {permInputMonitoring, true, false}}, got)
	assert.Regexp(t, "Privacy_ScreenCapture$", *r.Missing[1].SettingsURL)
	assert.True(t, r.Blocked())
	assert.Empty(t, r.Notes)
}

func TestPermissionsOptionalPermissionMissingDoesNotBlock(t *testing.T) {
	r := buildPermissionReport(platform.BackendMacOS, []string{permInputMonitoring}, nil, false)
	assert.False(t, r.Blocked())
	assert.False(t, NeedsOnboarding(true, r))
	assert.True(t, NeedsOnboarding(false, r))
}

func TestPermissionsWindowsAndX11HaveNoPermissions(t *testing.T) {
	for _, kind := range []platform.BackendKind{platform.BackendWindows, platform.BackendX11} {
		r := buildPermissionReport(kind, nil, []string{"X server lacks XTEST"}, false)
		assert.Empty(t, r.Missing, kind)
		assert.False(t, NeedsOnboarding(true, r), kind)
		assert.Equal(t, []string{"X server lacks XTEST"}, r.Notes, kind)
	}
}

func TestPermissionsNonMacosMissingCapabilitiesAreListedButNotRequired(t *testing.T) {
	r := buildPermissionReport(platform.BackendX11, []string{"foo"}, nil, false)
	assert.Equal(t, []ipc.PermissionInfo{{Kind: "foo"}}, r.Missing)
	assert.False(t, r.Blocked())
}

func TestPermissionsWaylandNotesExplainPortalTriggerAndLayerShell(t *testing.T) {
	capsNotes := []string{"Wayland-native: no passive input hook", noteWaylandPortal}
	r := buildPermissionReport(platform.BackendWayland, nil, capsNotes, false)
	assert.Equal(t, []string{noteWaylandPortal, noteWaylandTrigger, noteWaylandLayerShell, capsNotes[0]}, r.Notes,
		"duplicate capability notes are dropped")
	assert.Contains(t, noteWaylandTrigger, "divvy --trigger")
	assert.False(t, r.GNOME)
}

func TestPermissionsGNOMENoteIsAdded(t *testing.T) {
	r := buildPermissionReport(platform.BackendX11, nil, nil, true)
	assert.True(t, r.GNOME)
	assert.Equal(t, []string{noteGNOME}, r.Notes)
}

func TestPermissionsRecheckReportsChangesOnlyAfterBaseline(t *testing.T) {
	probe := &fakeProbe{}
	probe.setMissing(permAccessibility, permScreenRecording)
	p := newPermissions(platform.BackendMacOS, platform.Capabilities{}, false, probe)

	first, changed := p.recheck()
	assert.False(t, changed, "no baseline yet")
	assert.True(t, first.Blocked())
	_, changed = p.recheck()
	assert.False(t, changed, "unchanged")

	probe.setMissing(permScreenRecording)
	r, changed := p.recheck()
	assert.True(t, changed)
	assert.True(t, r.Missing[0].Granted)
	assert.True(t, r.Blocked())

	probe.setMissing()
	r, changed = p.recheck()
	assert.True(t, changed)
	assert.False(t, r.Blocked())
	assert.True(t, p.Current().Equal(r))
	_, changed = p.recheck()
	assert.False(t, changed, "unchanged after Current")
	assert.Equal(t, 6, probe.calls)
}

func TestPermissionsRequestOpensKnownMacosPanesOnly(t *testing.T) {
	probe := &fakeProbe{}
	p := newPermissions(platform.BackendMacOS, platform.Capabilities{}, false, probe)
	for _, k := range []string{permAccessibility, permInputMonitoring} {
		require.NoError(t, p.request(k))
	}
	err := p.request("camera")
	_, ok := errors.AsType[*permissionRequestError](err)
	require.True(t, ok, "%v", err)
	require.EqualError(t, err, "permission `camera` cannot be requested on this platform")
	assert.Equal(t, []string{permAccessibility, permInputMonitoring}, probe.opened)
}

func TestPermissionsRequestIsUnsupportedOffMacos(t *testing.T) {
	p := newPermissions(platform.BackendWayland, platform.Capabilities{}, false, staticProbe{})
	_, ok := errors.AsType[*permissionRequestError](p.request(permAccessibility))
	assert.True(t, ok, "expected a platform error")
}

func TestPermissionsStaticProbeUsesCapabilityNotes(t *testing.T) {
	caps := platform.Capabilities{Notes: []string{"note"}}
	r := newPermissions(platform.BackendX11, caps, false, staticProbe{"x"}).Current()
	assert.Equal(t, []string{"note"}, r.Notes)
	assert.Len(t, r.Missing, 1)
}

func TestPermissionsForBackendPicksTheProbe(t *testing.T) {
	caps := platform.Capabilities{MissingPermissions: []string{permAccessibility}}
	live := &fakeProbe{}
	r := PermissionsForBackend(platform.BackendMacOS, caps, false, live).Current()
	assert.False(t, r.Blocked(), "live probe on macOS")
	assert.Equal(t, 1, live.calls)
	r = PermissionsForBackend(platform.BackendMacOS, caps, false, nil).Current()
	assert.True(t, r.Blocked(), "static fallback on macOS")
	r = PermissionsForBackend(platform.BackendX11, caps, true, live).Current()
	assert.Equal(t, 1, live.calls, "static elsewhere")
	assert.Len(t, r.Missing, 1)
	assert.True(t, r.GNOME)
}

// The JSON goes to the frontend (PermissionReport / PermissionInfo / PermissionsChanged in
// web/src/bindings.ts): lists are never null.
func TestPermissionsJSON(t *testing.T) {
	testCases := []struct {
		name     string
		have     any
		expected string
	}{
		{
			"MacOS",
			ipc.PermissionsChanged{Report: buildPermissionReport(platform.BackendMacOS, []string{permScreenRecording}, nil, false)},
			`{"report":{"platform":"macos","missing":[` +
				`{"kind":"accessibility","granted":true,"required":true,"settings_url":"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"},` +
				`{"kind":"screen_recording","granted":false,"required":true,"settings_url":"x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"},` +
				`{"kind":"input_monitoring","granted":true,"required":false,"settings_url":"x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"}` +
				`],"notes":[],"gnome":false}}`,
		},
		{
			"X11WithoutPermissions",
			buildPermissionReport(platform.BackendX11, nil, nil, false),
			`{"platform":"x11","missing":[],"notes":[],"gnome":false}`,
		},
		{
			"WaylandGNOME",
			buildPermissionReport(platform.BackendWayland, []string{"foo"}, nil, true),
			`{"platform":"wayland","missing":[{"kind":"foo","granted":false,"required":false,"settings_url":null}],"notes":[` +
				`"Wayland-native: global hotkeys use the GlobalShortcuts desktop portal when the compositor offers it",` +
				"\"Wayland-native: without the portal, bind `divvy --trigger` (add `--pinned` for pinned) in your compositor\"," +
				`"Wayland-native: the overlay is anchored to the output with layer-shell, not to the game window",` +
				`"GNOME: only X11/XWayland is supported; run the game under XWayland"],"gnome":true}`,
		},
		{
			"ZeroValue",
			ipc.PermissionReport{Platform: platform.BackendWindows},
			`{"platform":"windows","missing":[],"notes":[],"gnome":false}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v2, err := json.Marshal(tc.have)
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(v2), "json/v2")
			v1, err := stdjson.Marshal(tc.have)
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(v1), "encoding/json")
		})
	}
}

func TestPermissionsJSONRoundTrips(t *testing.T) {
	r := buildPermissionReport(platform.BackendMacOS, []string{permAccessibility}, []string{"n"}, true)
	b, err := json.Marshal(ipc.PermissionsChanged{Report: r})
	require.NoError(t, err)
	var back ipc.PermissionsChanged
	require.NoError(t, json.Unmarshal(b, &back))
	assert.True(t, back.Report.Equal(r), "%+v", back.Report)
}

// Diagnostics says GNOME only when the permissions, built from the environment, do.
func TestDiagnosticsReportsGNOME(t *testing.T) {
	testCases := []struct {
		name        string
		permissions *Permissions
		expected    bool
	}{
		{"NoPermissions", nil, false},
		{"NotGNOME", newPermissions(platform.BackendX11, platform.Capabilities{}, false, staticProbe{}), false},
		{"GNOME", newPermissions(platform.BackendX11, platform.Capabilities{}, true, staticProbe{}), true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := newTestApp(t, platformtest.Options{})
			a.permissions = tc.permissions
			assert.Equal(t, tc.expected, a.diagnostics().GNOME)
		})
	}
}
