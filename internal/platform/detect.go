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

const (
	BackendWindows BackendKind = "windows"
	BackendMacos   BackendKind = "macos"
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
		return BackendMacos, true
	case "fake":
		return BackendFake, true
	}
	return "", false
}

// UnmarshalJSON accepts only the serde names.
func (k *BackendKind) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch v := BackendKind(s); v {
	case BackendWindows, BackendMacos, BackendX11, BackendWayland, BackendFake:
		*k = v
		return nil
	}
	return fmt.Errorf("unknown backend %q", s)
}

// EnvProbe is the environment snapshot [Detect] uses (injectable for tests).
type EnvProbe struct {
	// OS uses Rust's names (`linux`, `windows`, `macos`) so the detection rules read the same.
	OS              string
	XDGSessionType  string
	WaylandDisplay  string
	Display         string
	OverrideBackend string // RKL_BACKEND
	Desktop         string // XDG_CURRENT_DESKTOP
}

// EnvProbeFromEnv reads the probe from the process environment.
func EnvProbeFromEnv() EnvProbe {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	return EnvProbe{
		OS:              osName,
		XDGSessionType:  os.Getenv("XDG_SESSION_TYPE"),
		WaylandDisplay:  os.Getenv("WAYLAND_DISPLAY"),
		Display:         os.Getenv("DISPLAY"),
		OverrideBackend: os.Getenv("RKL_BACKEND"),
		Desktop:         os.Getenv("XDG_CURRENT_DESKTOP"),
	}
}

// IsGNOME reports whether XDG_CURRENT_DESKTOP contains GNOME (a colon-separated list, for
// example `ubuntu:GNOME`).
func (p EnvProbe) IsGNOME() bool {
	for part := range strings.SplitSeq(p.Desktop, ":") {
		if strings.EqualFold(part, "gnome") {
			return true
		}
	}
	return false
}

// Detect picks a backend. Order: explicit override (settings) > RKL_BACKEND > OS defaults.
// On Linux, X11/XWayland is preferred whenever DISPLAY is set; Wayland-native only when there
// is no X server, or when explicitly requested. GNOME with DISPLAY is X11 (XWayland); GNOME
// without DISPLAY is Wayland-native, since there is no X server for GTK to open.
func Detect(probe EnvProbe, settingsOverride *BackendKind) BackendKind {
	if settingsOverride != nil {
		return *settingsOverride
	}
	if kind, ok := ParseBackendKind(probe.OverrideBackend); ok {
		return kind
	}
	switch probe.OS {
	case "windows":
		return BackendWindows
	case "macos":
		return BackendMacos
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
