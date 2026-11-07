package mac

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestHeldModifiersFromCGFlags(t *testing.T) {
	assert.Empty(t, heldFromFlags(0))
	assert.Equal(t, []platform.KeyModifier{platform.Ctrl, platform.Shift}, heldFromFlags(flagControl|flagShift|0x100))
	assert.Equal(t, []platform.KeyModifier{platform.Alt, platform.Meta}, heldFromFlags(flagAlternate|flagCommand))
}

func win(number uint32, pid int32, title, owner *string) RawWindow {
	return RawWindow{Number: number, PID: pid, OwnerName: owner, Title: title, X: 10.4, Y: 20.6, Width: 1280, Height: 720}
}

func s(v string) *string { return &v }

func TestRectRoundsAndClamps(t *testing.T) {
	assert.Equal(t, platform.Rect{X: 10, Y: -21, Width: 100, Height: 0}, rectFromBounds(10.4, -20.6, 99.5, 0.2))
	assert.Equal(t, platform.Rect{X: math.MaxInt32, Y: math.MinInt32, Width: 0, Height: 0},
		rectFromBounds(1e12, -1e12, -5.0, math.NaN()))
}

func TestFindsGameByTitleAndFocus(t *testing.T) {
	windows := []RawWindow{win(1, 10, s("Path of Exile 2"), s("Path of Exile 2")), win(2, 11, s("Finder"), nil)}
	info, ok := findGame(windows)
	require.True(t, ok)
	assert.Equal(t, uint64(1), info.NativeID)
	assert.Equal(t, "Path of Exile 2", info.Title)
	assert.Equal(t, platform.Rect{X: 10, Y: 21, Width: 1280, Height: 720}, info.Rect)
	assert.True(t, info.Focused)
}

func TestGameBehindAnotherWindowIsNotFocused(t *testing.T) {
	windows := []RawWindow{win(2, 11, s("Safari"), nil), win(1, 10, s("Path of Exile 2"), nil)}
	info, ok := findGame(windows)
	require.True(t, ok)
	assert.False(t, info.Focused)
	assert.False(t, isFrontmost(windows, 10))
	assert.True(t, isFrontmost(windows, 11))
}

func TestOverlayLayersDoNotStealFocus(t *testing.T) {
	overlay := win(3, 99, s("rkl"), nil)
	overlay.Layer = 25
	info, ok := findGame([]RawWindow{overlay, win(1, 10, s("Path of Exile 2"), nil)})
	require.True(t, ok)
	assert.True(t, info.Focused)
	assert.False(t, isFrontmost(nil, 10))
}

func TestFallsBackToOwnerNameWithoutTitles(t *testing.T) {
	info, ok := findGame([]RawWindow{win(5, 10, nil, s("Path of Exile 2"))})
	require.True(t, ok)
	assert.Equal(t, platform.GameWindowTitle, info.Title)
	assert.Equal(t, uint64(5), info.NativeID)
	_, ok = findGame([]RawWindow{win(6, 10, nil, s("Safari"))})
	assert.False(t, ok)
	_, ok = findGame([]RawWindow{win(6, 10, nil, nil)})
	assert.False(t, ok)
}

func TestIgnoresWrongTitleLayerAndTinyWindows(t *testing.T) {
	// A visible title that differs wins over a matching owner name.
	_, ok := findGame([]RawWindow{win(1, 10, s("Path of Exile 2 Launcher"), s("Path of Exile 2"))})
	assert.False(t, ok)
	menu := win(1, 10, s("Path of Exile 2"), nil)
	menu.Layer = 24
	_, ok = findGame([]RawWindow{menu})
	assert.False(t, ok)
	tiny := win(1, 10, s("Path of Exile 2"), nil)
	tiny.Width = 10
	_, ok = findGame([]RawWindow{tiny})
	assert.False(t, ok)
	short := win(1, 10, s("Path of Exile 2"), nil)
	short.Height = 10
	assert.Nil(t, findGameRaw([]RawWindow{short}))
}

func info(id uint64, x int32, focused bool) *platform.WindowInfo {
	return &platform.WindowInfo{
		Title:    platform.GameWindowTitle,
		Rect:     platform.Rect{X: x, Y: 0, Width: 800, Height: 600},
		Focused:  focused,
		NativeID: id,
	}
}

func TestDiffCoversEveryTransition(t *testing.T) {
	assert.Empty(t, diff(nil, nil))
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowFound, Info: *info(1, 0, true)}}, diff(nil, info(1, 0, true)))
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowLost}}, diff(info(1, 0, true), nil))
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowFound, Info: *info(2, 0, true)}},
		diff(info(1, 0, true), info(2, 0, true)))
	assert.Empty(t, diff(info(1, 0, true), info(1, 0, true)))
	assert.Equal(t, []platform.WindowEvent{
		{Type: platform.WindowMoved, Rect: platform.Rect{X: 5, Y: 0, Width: 800, Height: 600}},
		{Type: platform.WindowFocusChanged, Focused: false},
	}, diff(info(1, 0, true), info(1, 5, false)))
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowFocusChanged, Focused: true}},
		diff(info(1, 0, false), info(1, 0, true)))
}

func TestPermissionsMapToCapabilities(t *testing.T) {
	none := capabilities(PermissionState{})
	assert.Equal(t, []string{"accessibility", "screen_recording", "input_monitoring"}, none.MissingPermissions)
	assert.True(t, none.WindowTracking)
	// Wails provides hotkeys and the clipboard; the shell sets those two.
	assert.False(t, none.GlobalHotkeys || none.Clipboard)
	assert.False(t, none.PassiveHook || none.KeystrokeInjection)
	assert.Len(t, none.Notes, 3)

	all := capabilities(PermissionState{Accessibility: true, ScreenRecording: true, InputMonitoring: true})
	assert.Empty(t, all.MissingPermissions)
	assert.Empty(t, all.Notes)
	assert.True(t, all.PassiveHook && all.KeystrokeInjection && all.OverlayAlwaysOnTop && all.OverlayClickThrough)

	// Accessibility alone is enough for the listen-only tap.
	ax := capabilities(PermissionState{Accessibility: true})
	assert.True(t, ax.PassiveHook && ax.KeystrokeInjection)
	assert.Equal(t, []string{"screen_recording", "input_monitoring"}, ax.MissingPermissions)

	im := PermissionState{InputMonitoring: true}
	assert.True(t, im.CanListen())
	assert.False(t, capabilities(im).KeystrokeInjection)
}

func TestSettingsURLs(t *testing.T) {
	u, ok := settingsURL(PermAccessibility)
	assert.True(t, ok && strings.HasSuffix(u, "Privacy_Accessibility"))
	u, ok = settingsURL(PermScreenRecording)
	assert.True(t, ok && strings.HasSuffix(u, "Privacy_ScreenCapture"))
	u, ok = settingsURL(PermInputMonitoring)
	assert.True(t, ok && strings.HasSuffix(u, "Privacy_ListenEvent"))
	_, ok = settingsURL("camera")
	assert.False(t, ok)
	for _, kind := range (PermissionState{}).Missing() {
		u, ok := settingsURL(kind)
		assert.True(t, ok && strings.HasPrefix(u, "x-apple.systempreferences:"), kind)
	}
}

func TestCopyModifierParseAndRemap(t *testing.T) {
	for in, want := range map[string]CopyModifier{
		" CMD ": CopyCommand, "meta": CopyCommand, "command": CopyCommand, "Control": CopyControl, "ctrl": CopyControl,
	} {
		got, ok := ParseCopyModifier(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	_, ok := ParseCopyModifier("hyper")
	assert.False(t, ok)
	var zero CopyModifier
	assert.Equal(t, CopyCommand, zero)

	copyChord := platform.NewChord("KeyC", platform.Ctrl, platform.Alt)
	assert.Equal(t, platform.NewChord("KeyC", platform.Meta, platform.Alt), remapChord(copyChord, CopyCommand))
	assert.Equal(t, copyChord, remapChord(copyChord, CopyControl))
	// Ctrl+Meta collapses to a single Meta instead of a duplicate.
	both := platform.NewChord("KeyC", platform.Meta, platform.Ctrl)
	assert.Equal(t, platform.NewChord("KeyC", platform.Meta), remapChord(both, CopyCommand))
}
