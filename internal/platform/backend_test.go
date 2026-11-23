package platform

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func linux() EnvProbe { return EnvProbe{OS: "linux"} }

func TestSettingsOverrideWinsOverEverything(t *testing.T) {
	probe := linux()
	probe.OverrideBackend, probe.Display = "wayland", ":0"
	assert.Equal(t, BackendFake, Detect(probe, BackendFake))
}

func TestEnvOverrideWinsOverOS(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected BackendKind
	}{
		{
			"X11",
			"x11",
			BackendX11,
		},
		{
			"XWayland",
			"XWayland",
			BackendX11,
		},
		{
			"Wayland",
			"Wayland",
			BackendWayland,
		},
		{
			"Windows",
			"windows",
			BackendWindows,
		},
		{
			"MacOS",
			"macos",
			BackendMacOS,
		},
		{
			"PaddedFake",
			" fake ",
			BackendFake,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			probe := linux()
			probe.OverrideBackend, probe.Display = tc.have, ":0"
			assert.Equal(t, tc.expected, Detect(probe, ""))
		})
	}
}

func TestUnknownEnvOverrideIsIgnored(t *testing.T) {
	probe := linux()
	probe.OverrideBackend, probe.Display = "bogus", ":0"
	assert.Equal(t, BackendX11, Detect(probe, ""))
}

func TestOSDefaults(t *testing.T) {
	assert.Equal(t, BackendWindows, Detect(EnvProbe{OS: "windows"}, ""))
	assert.Equal(t, BackendMacOS, Detect(EnvProbe{OS: "darwin"}, ""))
}

func TestLinuxPrefersX11WheneverDisplayIsSet(t *testing.T) {
	probe := EnvProbe{OS: "linux", Display: ":0", WaylandDisplay: "wayland-1", XDGSessionType: "wayland", Desktop: "sway"}
	assert.Equal(t, BackendX11, Detect(probe, ""))
}

func TestLinuxWaylandNativeWithoutDisplay(t *testing.T) {
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", WaylandDisplay: "wayland-1"}, ""))
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", XDGSessionType: "wayland"}, ""))
}

func TestGNOMEIsX11WhenXWaylandIsAvailable(t *testing.T) {
	probe := EnvProbe{OS: "linux", Display: ":0", WaylandDisplay: "wayland-0", Desktop: "ubuntu:GNOME"}
	assert.True(t, probe.IsGNOME())
	assert.Equal(t, BackendX11, Detect(probe, ""))
	// Without DISPLAY there is no X server: Wayland-native (X11 would fail GTK init).
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", WaylandDisplay: "wayland-0", Desktop: "GNOME"}, ""))
	assert.Equal(t, BackendWayland, Detect(EnvProbe{OS: "linux", XDGSessionType: "wayland", Desktop: "GNOME"}, ""))
}

func TestExplicitWaylandRequestIsHonouredOnGNOME(t *testing.T) {
	probe := EnvProbe{OS: "linux", Desktop: "GNOME", Display: ":0"}
	assert.Equal(t, BackendWayland, Detect(probe, BackendWayland))
}

func TestHasDesktopMatchesAnyListEntry(t *testing.T) {
	probe := EnvProbe{Desktop: "ubuntu:GNOME"}
	assert.True(t, probe.HasDesktop("gnome"))
	assert.True(t, probe.HasDesktop("Ubuntu"))
	assert.False(t, probe.HasDesktop("kde"))
	assert.False(t, EnvProbe{}.HasDesktop("gnome"))
}

func TestBareLinuxFallsBackToX11(t *testing.T) {
	assert.Equal(t, BackendX11, Detect(linux(), ""))
	assert.False(t, linux().IsGNOME())
}

func TestFromEnvReadsOS(t *testing.T) {
	assert.Equal(t, runtime.GOOS, EnvProbeFromEnv().OS)
}
