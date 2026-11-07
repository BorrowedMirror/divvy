package platform

import (
	"encoding/json"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLettersDigitsAndNamedKeys(t *testing.T) {
	cases := []struct {
		in   string
		want Chord
	}{
		{"Ctrl+Alt+D", NewChord("KeyD", Ctrl, Alt)},
		{"control + 1", NewChord("Digit1", Ctrl)},
		{"shift+F5", NewChord("F5", Shift)},
		{"cmd+option+KeyX", NewChord("KeyX", Meta, Alt)},
	}
	for _, c := range cases {
		got, err := ParseChord(c.in)
		require.NoError(t, err, c.in)
		assert.True(t, got.Equal(c.want), "%s: got %v", c.in, got)
	}
	got, err := ParseChord("super+win+d")
	require.NoError(t, err)
	assert.Equal(t, []KeyModifier{Meta, Meta}, got.Modifiers)
}

func TestParseRejectsBadChords(t *testing.T) {
	for _, s := range []string{"", "ctrl", "ctrl+alt", "a+b", "+++"} {
		_, err := ParseChord(s)
		assert.ErrorIs(t, err, ErrInvalidChord, s)
	}
}

func TestDisplayRoundtrips(t *testing.T) {
	for _, s := range []string{"Ctrl+Alt+D", "Shift+F5", "Meta+1", "Ctrl+Shift+Alt+Escape"} {
		c, err := ParseChord(s)
		require.NoError(t, err)
		assert.Equal(t, s, c.String())
		again, err := ParseChord(c.String())
		require.NoError(t, err)
		assert.True(t, again.Equal(c))
	}
}

func TestUnboundChord(t *testing.T) {
	assert.True(t, UnboundChord().IsUnbound())
	assert.True(t, NewChord("  ").IsUnbound())
	assert.False(t, NewChord("KeyD", Ctrl).IsUnbound())
	b, err := json.Marshal(UnboundChord())
	require.NoError(t, err)
	assert.JSONEq(t, `{"modifiers":[],"key":""}`, string(b))
}

func linux() EnvProbe { return EnvProbe{OS: "linux"} }

//go:fix inline
func ptr[T any](v T) *T { return new(v) }

func TestSettingsOverrideWinsOverEverything(t *testing.T) {
	probe := linux()
	probe.OverrideBackend, probe.Display = "wayland", ":0"
	assert.Equal(t, BackendFake, Detect(probe, new(BackendFake)))
}

func TestEnvOverrideWinsOverOS(t *testing.T) {
	for v, k := range map[string]BackendKind{
		"x11": BackendX11, "XWayland": BackendX11, "Wayland": BackendWayland,
		"windows": BackendWindows, "macos": BackendMacos, " fake ": BackendFake,
	} {
		probe := linux()
		probe.OverrideBackend, probe.Display = v, ":0"
		assert.Equal(t, k, Detect(probe, nil), v)
	}
}

func TestUnknownEnvOverrideIsIgnored(t *testing.T) {
	probe := linux()
	probe.OverrideBackend, probe.Display = "bogus", ":0"
	assert.Equal(t, BackendX11, Detect(probe, nil))
}

func TestOSDefaults(t *testing.T) {
	assert.Equal(t, BackendWindows, Detect(EnvProbe{OS: "windows"}, nil))
	assert.Equal(t, BackendMacos, Detect(EnvProbe{OS: "macos"}, nil))
}

func TestLinuxPrefersX11WheneverDisplayIsSet(t *testing.T) {
	probe := EnvProbe{OS: "linux", Display: ":0", WaylandDisplay: "wayland-1", XDGSessionType: "wayland", Desktop: "sway"}
	assert.Equal(t, BackendX11, Detect(probe, nil))
}

func TestLinuxWaylandNativeWithoutDisplay(t *testing.T) {
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", WaylandDisplay: "wayland-1"}, nil))
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", XDGSessionType: "wayland"}, nil))
}

func TestGNOMEIsX11WhenXWaylandIsAvailable(t *testing.T) {
	probe := EnvProbe{OS: "linux", Display: ":0", WaylandDisplay: "wayland-0", Desktop: "ubuntu:GNOME"}
	assert.True(t, probe.IsGNOME())
	assert.Equal(t, BackendX11, Detect(probe, nil))
	// Without DISPLAY there is no X server: Wayland-native (X11 would fail GTK init).
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", WaylandDisplay: "wayland-0", Desktop: "GNOME"}, nil))
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", XDGSessionType: "wayland", Desktop: "GNOME"}, nil))
}

func TestExplicitWaylandRequestIsHonouredOnGNOME(t *testing.T) {
	probe := EnvProbe{OS: "linux", Desktop: "GNOME", Display: ":0"}
	assert.Equal(t, BackendWayland, Detect(probe, new(BackendWayland)))
}

func TestBareLinuxFallsBackToX11(t *testing.T) {
	assert.Equal(t, BackendX11, Detect(linux(), nil))
	assert.False(t, linux().IsGNOME())
}

func TestFromEnvReadsOS(t *testing.T) {
	want := runtime.GOOS
	if want == "darwin" {
		want = "macos"
	}
	assert.Equal(t, want, EnvProbeFromEnv().OS)
}

// The JSON below is what serde writes for the Rust types.
func TestSerdeShapes(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{Action(PriceCheck), `"price_check"`},
		{Action(PriceCheckPinned), `"price_check_pinned"`},
		{ChatAction(2), `{"chat":2}`},
		{StashSearchAction(0), `{"stash_search":0}`},
		{HotkeyEvent{Action: Action(OpenTrade)}, `{"action":"open_trade"}`},
		{WindowEvent{Type: WindowFound, Info: WindowInfo{Title: "Path of Exile 2", Rect: Rect{1, 2, 3, 4}, Focused: true, NativeID: 9}},
			`{"type":"found","title":"Path of Exile 2","rect":{"x":1,"y":2,"width":3,"height":4},"focused":true,"native_id":9}`},
		{WindowEvent{Type: WindowMoved, Rect: Rect{-5, 0, 10, 10}}, `{"type":"moved","rect":{"x":-5,"y":0,"width":10,"height":10}}`},
		{WindowEvent{Type: WindowFocusChanged, Focused: false}, `{"type":"focus_changed","focused":false}`},
		{WindowEvent{Type: WindowLost}, `{"type":"lost"}`},
		{InputEvent{Type: KeyDown, Key: "Escape"}, `{"type":"key_down","key":"Escape"}`},
		{InputEvent{Type: Wheel, Delta: -1}, `{"type":"wheel","delta":-1}`},
		{Capabilities{}, `{"window_tracking":false,"overlay_always_on_top":false,"overlay_click_through":false,"global_hotkeys":false,"passive_hook":false,"keystroke_injection":false,"clipboard":false,"missing_permissions":[],"notes":[]}`},
		{BackendX11, `"x11"`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.v)
		require.NoError(t, err)
		assert.Equal(t, c.want, string(b))
	}
}

func TestSerdeRoundTrips(t *testing.T) {
	for _, a := range []HotkeyAction{Action(PriceCheck), Action(OpenSettings), ChatAction(7), StashSearchAction(3)} {
		b, err := json.Marshal(a)
		require.NoError(t, err)
		var back HotkeyAction
		require.NoError(t, json.Unmarshal(b, &back))
		assert.Equal(t, a, back)
	}
	var bad HotkeyAction
	assert.Error(t, json.Unmarshal([]byte(`"nope"`), &bad))
	assert.Error(t, json.Unmarshal([]byte(`{"chat":1,"stash_search":2}`), &bad))

	for _, e := range []WindowEvent{
		{Type: WindowFound, Info: WindowInfo{Title: "x", Rect: Rect{1, 2, 3, 4}, NativeID: 5}},
		{Type: WindowMoved, Rect: Rect{1, 1, 1, 1}},
		{Type: WindowFocusChanged, Focused: true},
		{Type: WindowLost},
	} {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		var back WindowEvent
		require.NoError(t, json.Unmarshal(b, &back))
		assert.Equal(t, e, back)
	}

	var k BackendKind
	require.NoError(t, json.Unmarshal([]byte(`"wayland"`), &k))
	assert.Equal(t, BackendWayland, k)
	assert.Error(t, json.Unmarshal([]byte(`"Wayland"`), &k))
}

func TestErrorKindsAndMessages(t *testing.T) {
	err := Timeout(2 * time.Second)
	assert.ErrorIs(t, err, ErrTimeout)
	assert.NotErrorIs(t, err, ErrBackend)
	assert.Equal(t, "timed out after 2s", err.Error())
	assert.Equal(t, "invalid key chord `a+b`", InvalidChord("a+b").Error())
	assert.Equal(t, "capability `global_hotkeys` is not supported on this backend", Unsupported("global_hotkeys").Error())
	wrapped := errors.Join(errors.New("context"), HotkeyConflict("Ctrl+D"))
	assert.ErrorIs(t, wrapped, ErrHotkeyConflict)
}
