package wayland

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestInjectionIsAvailableOnDemand(t *testing.T) {
	e := platform.Backend("enigo: no keymap")
	assert.Equal(t, injection{available: true}, injectionPlan(nil, false, false))
	p := injectionPlan(e, true, false)
	assert.True(t, p.available && strings.Contains(p.note, "ydotool"), p)
	p = injectionPlan(e, false, true)
	assert.True(t, p.available && strings.Contains(p.note, "retried"), p)
	p = injectionPlan(e, false, false)
	assert.True(t, !p.available && strings.Contains(p.note, "no ydotool"), p)
}

func TestVirtualKeyboardPlausibilityByDesktop(t *testing.T) {
	probe := func(d string) platform.EnvProbe { return platform.EnvProbe{Desktop: d} }
	assert.True(t, virtualKeyboardPlausible(probe("sway")))
	assert.True(t, virtualKeyboardPlausible(platform.EnvProbe{}))
	assert.False(t, virtualKeyboardPlausible(probe("ubuntu:GNOME")))
	assert.False(t, virtualKeyboardPlausible(probe("KDE")))
}

func TestCapabilitiesAndNotes(t *testing.T) {
	caps := capabilities(platform.EnvProbe{Desktop: "sway"})
	assert.False(t, caps.WindowTracking || caps.PassiveHook)
	assert.True(t, caps.OverlayAlwaysOnTop && caps.OverlayClickThrough)
	assert.True(t, slices.ContainsFunc(caps.Notes, func(n string) bool { return strings.Contains(n, "anchored to the output") }))
	assert.Len(t, caps.Notes, 2)

	gnome := capabilities(platform.EnvProbe{Desktop: "ubuntu:GNOME"})
	assert.False(t, gnome.OverlayAlwaysOnTop)
	assert.Contains(t, gnome.Notes, noteGNOME)

	assert.Empty(t, HotkeyNotes(true))
	require.Len(t, HotkeyNotes(false), 1)
	assert.Contains(t, HotkeyNotes(false)[0], "--trigger")
}

func TestGDKBackendMatchesPlatformBackend(t *testing.T) {
	assert.Equal(t, "x11", GDKBackendFor(platform.BackendX11))
	assert.Equal(t, "x11", GDKBackendFor(platform.BackendFake))
	assert.Equal(t, "wayland", GDKBackendFor(platform.BackendWayland))
}

func TestAnchorForPicksTheNearerEdge(t *testing.T) {
	area := platform.Rect{X: 1920, Y: 0, Width: 1920, Height: 1080}
	// Right half: anchored right, side = gap to the right edge.
	assert.Equal(t, Anchor{Right: true, Top: 100, Side: 20},
		AnchorFor(platform.Rect{X: 1920 + 1920 - 420 - 20, Y: 100, Width: 420, Height: 500}, area))
	// Left half: anchored left, side = gap to the left edge.
	assert.Equal(t, Anchor{Right: false, Top: 40, Side: 30},
		AnchorFor(platform.Rect{X: 1950, Y: 40, Width: 420, Height: 500}, area))
	// Overhanging the edge clamps the margin to 0.
	assert.Equal(t, Anchor{Right: true, Top: 0, Side: 0},
		AnchorFor(platform.Rect{X: 3800, Y: 0, Width: 420, Height: 500}, area))
}
