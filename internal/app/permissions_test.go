package app

import (
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
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
	r := BuildPermissionReport(platform.BackendMacos, []string{PermScreenRecording}, nil, false)
	type row struct {
		kind              string
		granted, required bool
	}
	var got []row
	for _, p := range r.Missing {
		got = append(got, row{p.Kind, p.Granted, p.Required})
	}
	want := []row{{PermAccessibility, true, true}, {PermScreenRecording, false, true}, {PermInputMonitoring, true, false}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
	for _, p := range r.Missing {
		if p.SettingsURL == nil || !strings.HasPrefix(*p.SettingsURL, "x-apple.systempreferences:") {
			t.Fatalf("deep link %v", p.SettingsURL)
		}
	}
	if !strings.HasSuffix(*r.Missing[1].SettingsURL, "Privacy_ScreenCapture") {
		t.Fatal(*r.Missing[1].SettingsURL)
	}
	if !r.Blocked() || len(r.Notes) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestPermissionsOptionalPermissionMissingDoesNotBlock(t *testing.T) {
	r := BuildPermissionReport(platform.BackendMacos, []string{PermInputMonitoring}, nil, false)
	if r.Blocked() || NeedsOnboarding(true, r) || !NeedsOnboarding(false, r) {
		t.Fatalf("%+v", r)
	}
}

func TestPermissionsWindowsAndX11HaveNoPermissions(t *testing.T) {
	for _, kind := range []platform.BackendKind{platform.BackendWindows, platform.BackendX11} {
		r := BuildPermissionReport(kind, nil, []string{"X server lacks XTEST"}, false)
		if len(r.Missing) != 0 || r.Blocked() || NeedsOnboarding(true, r) {
			t.Fatalf("%s: %+v", kind, r)
		}
		if !slices.Equal(r.Notes, []string{"X server lacks XTEST"}) {
			t.Fatalf("%s: %q", kind, r.Notes)
		}
	}
}

func TestPermissionsNonMacosMissingCapabilitiesAreListedButNotRequired(t *testing.T) {
	r := BuildPermissionReport(platform.BackendX11, []string{"foo"}, nil, false)
	if len(r.Missing) != 1 || !r.Missing[0].equal(PermissionInfo{Kind: "foo"}) {
		t.Fatalf("%+v", r.Missing)
	}
	if r.Blocked() {
		t.Fatal("blocked")
	}
}

func TestPermissionsWaylandNotesExplainPortalTriggerAndLayerShell(t *testing.T) {
	capsNotes := []string{"Wayland-native: no passive input hook", NoteWaylandPortal}
	r := BuildPermissionReport(platform.BackendWayland, nil, capsNotes, false)
	if !slices.Equal(r.Notes[:3], []string{NoteWaylandPortal, NoteWaylandTrigger, NoteWaylandLayerShell}) {
		t.Fatalf("%q", r.Notes)
	}
	if !slices.ContainsFunc(r.Notes, func(n string) bool { return strings.Contains(n, "rkl --trigger") }) {
		t.Fatal("trigger note")
	}
	if len(r.Notes) != 4 {
		t.Fatalf("duplicate capability notes are dropped: %q", r.Notes)
	}
	if r.GNOME {
		t.Fatal("gnome")
	}
}

func TestPermissionsGNOMENoteIsAdded(t *testing.T) {
	r := BuildPermissionReport(platform.BackendX11, nil, nil, true)
	if !r.GNOME || !slices.Equal(r.Notes, []string{NoteGNOME}) {
		t.Fatalf("%+v", r)
	}
}

func TestPermissionsRecheckReportsChangesOnlyAfterBaseline(t *testing.T) {
	probe := &fakeProbe{}
	probe.setMissing(PermAccessibility, PermScreenRecording)
	p := NewPermissions(platform.BackendMacos, platform.Capabilities{}, false, probe)

	first, changed := p.Recheck()
	if changed {
		t.Fatal("no baseline yet")
	}
	if !first.Blocked() {
		t.Fatal("blocked")
	}
	if _, changed := p.Recheck(); changed {
		t.Fatal("unchanged")
	}

	probe.setMissing(PermScreenRecording)
	r, changed := p.Recheck()
	if !changed || !r.Missing[0].Granted || !r.Blocked() {
		t.Fatalf("%v %+v", changed, r)
	}

	probe.setMissing()
	r, changed = p.Recheck()
	if !changed || r.Blocked() {
		t.Fatalf("%v %+v", changed, r)
	}
	if !p.Current().Equal(r) {
		t.Fatal("current")
	}
	if _, changed := p.Recheck(); changed {
		t.Fatal("unchanged after current")
	}
	if probe.calls != 6 {
		t.Fatalf("calls = %d", probe.calls)
	}
}

func TestPermissionsRequestOpensKnownMacosPanesOnly(t *testing.T) {
	probe := &fakeProbe{}
	p := NewPermissions(platform.BackendMacos, platform.Capabilities{}, false, probe)
	for _, k := range []string{PermAccessibility, PermInputMonitoring} {
		if err := p.Request(k); err != nil {
			t.Fatal(err)
		}
	}
	err := p.Request("camera")
	reqErr, ok := errors.AsType[*PermissionRequestError](err)
	if !ok || reqErr.IPCKind() != "platform" {
		t.Fatalf("%v", err)
	}
	if err.Error() != "permission `camera` cannot be requested on this platform" {
		t.Fatal(err)
	}
	if !slices.Equal(probe.opened, []string{PermAccessibility, PermInputMonitoring}) {
		t.Fatalf("%q", probe.opened)
	}
}

func TestPermissionsRequestIsUnsupportedOffMacos(t *testing.T) {
	p := NewPermissions(platform.BackendWayland, platform.Capabilities{}, false, StaticProbe{})
	if _, ok := errors.AsType[*PermissionRequestError](p.Request(PermAccessibility)); !ok {
		t.Fatal("expected a platform error")
	}
	if !strings.Contains(p.String(), "wayland") {
		t.Fatal(p.String())
	}
}

func TestPermissionsStaticProbeUsesCapabilityNotes(t *testing.T) {
	caps := platform.Capabilities{Notes: []string{"note"}}
	p := NewPermissions(platform.BackendX11, caps, false, StaticProbe{"x"})
	r := p.Current()
	if !slices.Equal(r.Notes, []string{"note"}) || len(r.Missing) != 1 {
		t.Fatalf("%+v", r)
	}
	StaticProbe{}.OpenSettings("x")
}

func TestPermissionsForBackendPicksTheProbe(t *testing.T) {
	caps := platform.Capabilities{MissingPermissions: []string{PermAccessibility}}
	live := &fakeProbe{}
	if r := PermissionsForBackend(platform.BackendMacos, caps, false, live).Current(); r.Blocked() || live.calls != 1 {
		t.Fatalf("live probe on macOS: %+v", r)
	}
	if r := PermissionsForBackend(platform.BackendMacos, caps, false, nil).Current(); !r.Blocked() {
		t.Fatalf("static fallback on macOS: %+v", r)
	}
	if r := PermissionsForBackend(platform.BackendX11, caps, true, live).Current(); live.calls != 1 || len(r.Missing) != 1 || !r.GNOME {
		t.Fatalf("static elsewhere: %+v", r)
	}
}

// The JSON goes to the frontend: it must be byte-identical to serde (see
// PermissionReport_Serialize / PermissionInfo_Serialize / PermissionsChanged_Serialize in
// web/src/bindings.ts).
func TestPermissionsJSONMatchesSerde(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{
			"macos",
			PermissionsChanged{Report: BuildPermissionReport(platform.BackendMacos, []string{PermScreenRecording}, nil, false)},
			`{"report":{"platform":"macos","missing":[` +
				`{"kind":"accessibility","granted":true,"required":true,"settings_url":"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"},` +
				`{"kind":"screen_recording","granted":false,"required":true,"settings_url":"x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"},` +
				`{"kind":"input_monitoring","granted":true,"required":false,"settings_url":"x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"}` +
				`],"notes":[],"gnome":false}}`,
		},
		{
			"x11 without permissions",
			BuildPermissionReport(platform.BackendX11, nil, nil, false),
			`{"platform":"x11","missing":[],"notes":[],"gnome":false}`,
		},
		{
			"wayland gnome",
			BuildPermissionReport(platform.BackendWayland, []string{"foo"}, nil, true),
			`{"platform":"wayland","missing":[{"kind":"foo","granted":false,"required":false,"settings_url":null}],"notes":[` +
				`"Wayland-native: global hotkeys use the GlobalShortcuts desktop portal when the compositor offers it",` +
				"\"Wayland-native: without the portal, bind `rkl --trigger` (add `--pinned` for pinned) in your compositor\"," +
				`"Wayland-native: the overlay is anchored to the output with layer-shell, not to the game window",` +
				`"GNOME: only X11/XWayland is supported; run the game under XWayland"],"gnome":true}`,
		},
		{
			"zero value",
			PermissionReport{Platform: platform.BackendWindows},
			`{"platform":"windows","missing":[],"notes":[],"gnome":false}`,
		},
	}
	for _, c := range cases {
		v2, err := json.Marshal(c.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(v2) != c.want {
			t.Errorf("%s json/v2:\n got %s\nwant %s", c.name, v2, c.want)
		}
		serde, err := core.MarshalSerde(c.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(serde) != c.want {
			t.Errorf("%s MarshalSerde:\n got %s\nwant %s", c.name, serde, c.want)
		}
		if c.name == "zero value" {
			continue // encoding/json (v1) writes nil slices as null
		}
		v1, err := stdjson.Marshal(c.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(v1) != c.want {
			t.Errorf("%s encoding/json:\n got %s\nwant %s", c.name, v1, c.want)
		}
	}
}

func TestPermissionsJSONRoundTrips(t *testing.T) {
	r := BuildPermissionReport(platform.BackendMacos, []string{PermAccessibility}, []string{"n"}, true)
	b, err := json.Marshal(PermissionsChanged{Report: r})
	if err != nil {
		t.Fatal(err)
	}
	var back PermissionsChanged
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Report.Equal(r) {
		t.Fatalf("%+v", back.Report)
	}
}
