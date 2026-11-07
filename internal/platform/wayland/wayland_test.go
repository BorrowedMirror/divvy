//go:build linux

package wayland

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestRequiresWaylandDisplay(t *testing.T) {
	_, err := New(WithProbe(platform.EnvProbe{OS: "linux"}))
	assert.True(t, errors.Is(err, platform.ErrUnsupported), err)
}

func TestOverlayOverNilHandleIsUnsupported(t *testing.T) {
	parts, err := New(WithProbe(platform.EnvProbe{OS: "linux", WaylandDisplay: "rkl-no-such-display"}),
		withDisplay("rkl-no-such-display"))
	require.NoError(t, err)
	_, err = parts.Overlay(unsafe.Pointer(nil))
	assert.True(t, errors.Is(err, platform.ErrUnsupported), err)
}

// withDisplay points the virtual keyboard at another Wayland display (tests).
func withDisplay(name string) Option {
	return func(o *options) { o.display = name; o.noYdotool = true }
}

func TestUnreachableCompositorReportsItself(t *testing.T) {
	parts, err := New(WithProbe(platform.EnvProbe{OS: "linux", WaylandDisplay: "x", Desktop: "sway"}),
		withDisplay("rkl-no-such-display"))
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
	_, err = parts.WindowTracker.Subscribe()
	assert.True(t, errors.Is(err, platform.ErrUnsupported))
	_, err = parts.InputListener.Start()
	assert.True(t, errors.Is(err, platform.ErrUnsupported))

	// GNOME: implausible and no ydotool, so unsupported.
	parts, err = New(WithProbe(platform.EnvProbe{OS: "linux", WaylandDisplay: "x", Desktop: "GNOME"}),
		withDisplay("rkl-no-such-display"))
	require.NoError(t, err)
	assert.False(t, parts.Capabilities.KeystrokeInjection)
	err = parts.InputSimulator.SendChord(platform.NewChord("KeyC", platform.Ctrl))
	assert.True(t, errors.Is(err, platform.ErrUnsupported), err)
}

type countingSim struct{ n int }

func (c *countingSim) SendChord(platform.Chord) error { c.n++; return nil }

func TestLazyVirtualKeyboardRetriesAndUsesFallback(t *testing.T) {
	vk := &virtualKeyboard{display: "rkl-no-such-display"}
	chord := platform.NewChord("KeyC", platform.Ctrl)
	assert.Error(t, vk.ensureReady())
	err := vk.SendChord(chord)
	assert.True(t, errors.Is(err, platform.ErrBackend), "retried, still failing: %v", err)
	assert.False(t, vk.ready())
	err = vk.SendChord(platform.NewChord("NoSuchKey"))
	assert.True(t, errors.Is(err, platform.ErrInvalidChord), err)

	fallback := &countingSim{}
	vk = &virtualKeyboard{display: "rkl-no-such-display", fallback: fallback}
	require.NoError(t, vk.SendChord(chord))
	require.NoError(t, vk.SendChord(chord))
	assert.Equal(t, 2, fallback.n)
}
