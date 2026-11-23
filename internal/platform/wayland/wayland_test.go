//go:build linux

package wayland

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestRequiresWaylandDisplay(t *testing.T) {
	_, err := New(Options{Probe: platform.EnvProbe{OS: "linux"}})
	require.ErrorIs(t, err, platform.ErrUnsupported)
}

func TestOverlayOverNilHandleIsUnsupported(t *testing.T) {
	parts, err := New(testOptions(platform.EnvProbe{OS: "linux", WaylandDisplay: "divvy-no-such-display"}))
	require.NoError(t, err)
	_, err = parts.Overlay(unsafe.Pointer(nil))
	require.ErrorIs(t, err, platform.ErrUnsupported)
}

// testOptions points the virtual keyboard at a display that does not exist and turns ydotool
// off.
func testOptions(probe platform.EnvProbe) Options {
	return Options{Probe: probe, display: "divvy-no-such-display", noYdotool: true}
}

func TestUnreachableCompositorReportsItself(t *testing.T) {
	parts, err := New(testOptions(platform.EnvProbe{OS: "linux", WaylandDisplay: "x", Desktop: "sway"}))
	require.NoError(t, err)
	assert.Equal(t, platform.BackendWayland, parts.Kind)
	caps := parts.Capabilities
	assert.False(t, caps.WindowTracking || caps.PassiveHook || caps.GlobalHotkeys || caps.Clipboard)
	// sway is plausible: available on demand, retried on each copy.
	assert.True(t, caps.KeystrokeInjection)
	assert.Contains(t, caps.Notes[len(caps.Notes)-1], "retried on each copy")
	assert.Nil(t, parts.Cursor)
	assert.Nil(t, parts.ActivateWindow)

	_, found := parts.WindowTracker.GameWindow()
	assert.False(t, found)
	assert.True(t, parts.WindowTracker.IsGameFocused())
	_, err = parts.WindowTracker.Subscribe(t.Context())
	require.ErrorIs(t, err, platform.ErrUnsupported)
	_, err = parts.InputListener.Start(t.Context())
	require.ErrorIs(t, err, platform.ErrUnsupported)

	// GNOME: implausible and no ydotool, so unsupported.
	parts, err = New(testOptions(platform.EnvProbe{OS: "linux", WaylandDisplay: "x", Desktop: "GNOME"}))
	require.NoError(t, err)
	assert.False(t, parts.Capabilities.KeystrokeInjection)
	err = parts.InputSimulator.SendChord(platform.NewChord("KeyC", platform.Ctrl))
	require.ErrorIs(t, err, platform.ErrUnsupported)
}

type countingSim struct{ n int }

func (c *countingSim) SendChord(platform.Chord) error { c.n++; return nil }

func TestLazyVirtualKeyboardRetriesAndUsesFallback(t *testing.T) {
	vk := &virtualKeyboard{display: "divvy-no-such-display"}
	chord := platform.NewChord("KeyC", platform.Ctrl)
	require.Error(t, vk.ensureReady())
	err := vk.SendChord(chord)
	require.ErrorIs(t, err, platform.ErrBackend, "retried, still failing")
	assert.False(t, vk.ready())
	err = vk.SendChord(platform.NewChord("NoSuchKey"))
	require.ErrorIs(t, err, platform.ErrInvalidChord)

	fallback := &countingSim{}
	vk = &virtualKeyboard{display: "divvy-no-such-display", fallback: fallback}
	require.NoError(t, vk.SendChord(chord))
	require.NoError(t, vk.SendChord(chord))
	assert.Equal(t, 2, fallback.n)
}
