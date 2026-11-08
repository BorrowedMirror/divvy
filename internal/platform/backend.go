package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// BackendKind is the OS integration backend in use.
type BackendKind string

// Backends.
const (
	BackendWindows BackendKind = "windows"
	BackendMacOS   BackendKind = "macos"
	// BackendX11 is X11 or XWayland (the default on Linux).
	BackendX11 BackendKind = "x11"
	// BackendWayland is Wayland-native (layer-shell and portals).
	BackendWayland BackendKind = "wayland"
	// BackendFake is in-process fakes (tests, `RKL_BACKEND=fake`).
	BackendFake BackendKind = "fake"
)

// ParseBackendKind parses a `RKL_BACKEND` value (`x11|wayland|windows|macos|fake`,
// case-insensitive).
func ParseBackendKind(s string) (BackendKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "x11", "xwayland":
		return BackendX11, true
	case "wayland":
		return BackendWayland, true
	case "windows":
		return BackendWindows, true
	case "macos":
		return BackendMacOS, true
	case "fake":
		return BackendFake, true
	}
	return "", false
}

// UnmarshalJSON accepts only the exact backend names.
func (k *BackendKind) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch v := BackendKind(s); v {
	case BackendWindows, BackendMacOS, BackendX11, BackendWayland, BackendFake:
		*k = v
		return nil
	}
	return fmt.Errorf("unknown backend %q", s)
}

// EnvProbe is the environment snapshot [Detect] uses (injectable for tests).
type EnvProbe struct {
	// OS is a GOOS value (`linux`, `windows`, `darwin`).
	OS              string
	XDGSessionType  string
	WaylandDisplay  string
	Display         string
	OverrideBackend string // RKL_BACKEND
	Desktop         string // XDG_CURRENT_DESKTOP
}

// EnvProbeFromEnv reads the probe from the process environment.
func EnvProbeFromEnv() EnvProbe {
	return EnvProbe{
		OS:              runtime.GOOS,
		XDGSessionType:  os.Getenv("XDG_SESSION_TYPE"),
		WaylandDisplay:  os.Getenv("WAYLAND_DISPLAY"),
		Display:         os.Getenv("DISPLAY"),
		OverrideBackend: os.Getenv("RKL_BACKEND"),
		Desktop:         os.Getenv("XDG_CURRENT_DESKTOP"),
	}
}

// HasDesktop reports whether XDG_CURRENT_DESKTOP (a colon-separated list, for example
// `ubuntu:GNOME`) names desktop, ignoring case.
func (p EnvProbe) HasDesktop(desktop string) bool {
	for part := range strings.SplitSeq(p.Desktop, ":") {
		if strings.EqualFold(part, desktop) {
			return true
		}
	}
	return false
}

// IsGNOME reports whether the desktop is GNOME.
func (p EnvProbe) IsGNOME() bool { return p.HasDesktop("gnome") }

// Detect picks a backend. Order: explicit override (settings; empty for none) > RKL_BACKEND >
// OS defaults. On Linux, X11/XWayland is preferred whenever DISPLAY is set; Wayland-native only
// when there is no X server, or when explicitly requested. GNOME with DISPLAY is X11
// (XWayland); GNOME without DISPLAY is Wayland-native, since there is no X server for GTK to
// open.
func Detect(probe EnvProbe, override BackendKind) BackendKind {
	if override != "" {
		return override
	}
	if kind, ok := ParseBackendKind(probe.OverrideBackend); ok {
		return kind
	}
	switch probe.OS {
	case "windows":
		return BackendWindows
	case "darwin":
		return BackendMacOS
	}
	return linuxDefault(probe)
}

func linuxDefault(probe EnvProbe) BackendKind {
	// GNOME (mutter) implements neither layer-shell nor wlr virtual-keyboard, so the
	// Wayland-native backend is crippled there: prefer XWayland whenever DISPLAY is set.
	// Without DISPLAY (XWayland disabled) the X11 backend, and GTK's X11 init, would fail.
	if probe.Display != "" {
		return BackendX11
	}
	if probe.WaylandDisplay != "" || probe.XDGSessionType == "wayland" {
		return BackendWayland
	}
	return BackendX11
}
