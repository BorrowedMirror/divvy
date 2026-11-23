package wayland

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestInjectionIsAvailableOnDemand(t *testing.T) {
	e := platform.Backend("virtual keyboard: no keymap")
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

	assert.Empty(t, hotkeyNotes(true))
	require.Len(t, hotkeyNotes(false), 1)
	assert.Contains(t, hotkeyNotes(false)[0], "--trigger")
}
