package wayland

import (
	"fmt"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Capability notes, shown on the diagnostics page.
const (
	noteNoTracking = "Wayland-native: game window cannot be tracked; overlay anchored to the output"
	noteNoHook     = "Wayland-native: no passive input hook"
	noteGNOME      = "GNOME has no layer-shell: the overlay cannot stay above the game; use X11/XWayland"
	noteNoPortal   = "No GlobalShortcuts portal: bind `rkl --trigger` (or `pkill -USR2 rkl`; `-USR1` to pin) in " +
		"your compositor"
)

// capabilities is everything [New] reports except keystroke injection (see [injectionPlan]);
// GlobalHotkeys and Clipboard are the shell's.
func capabilities(probe platform.EnvProbe) platform.Capabilities {
	gnome := probe.IsGNOME()
	caps := platform.Capabilities{
		WindowTracking:      false,
		OverlayAlwaysOnTop:  !gnome,
		OverlayClickThrough: true,
		PassiveHook:         false,
		MissingPermissions:  []string{},
		Notes:               []string{noteNoTracking, noteNoHook},
	}
	if gnome {
		caps.Notes = append(caps.Notes, noteGNOME)
	}
	return caps
}

// HotkeyNotes are the notes for the shell's GlobalHotkeys capability: the signal fallback when
// the GlobalShortcuts portal is missing.
func HotkeyNotes(portal bool) []string {
	if portal {
		return nil
	}
	return []string{noteNoPortal}
}

// virtualKeyboardPlausible reports whether the compositor may implement
// zwp_virtual_keyboard_v1 (wlroots-based ones do; mutter and KWin do not).
func virtualKeyboardPlausible(probe platform.EnvProbe) bool {
	kde := false
	for part := range strings.SplitSeq(probe.Desktop, ":") {
		if strings.EqualFold(part, "kde") {
			kde = true
		}
	}
	return !probe.IsGNOME() && !kde
}

// injection is the keystroke-injection capability and its diagnostics note.
type injection struct {
	// available means "available on demand": the virtual keyboard is retried on each send
	// until it works.
	available bool
	note      string
}

// injectionPlan decides the capability. eagerErr is why the virtual keyboard failed at startup
// (nil = it works now).
func injectionPlan(eagerErr error, ydotool, plausible bool) injection {
	switch {
	case eagerErr == nil:
		return injection{available: true}
	case ydotool:
		return injection{available: true, note: fmt.Sprintf(
			"Virtual keyboard unavailable at startup (%v); retried on each copy, ydotool (needs ydotoold) "+
				"used meanwhile", eagerErr)}
	case plausible:
		return injection{available: true, note: fmt.Sprintf(
			"Virtual keyboard (virtual-keyboard protocol) not ready at startup (%v); retried on each copy", eagerErr)}
	default:
		return injection{available: false, note: fmt.Sprintf("No virtual-keyboard protocol and no ydotool: %v", eagerErr)}
	}
}

// GDKBackendFor is the GDK_BACKEND matching a platform backend. The shell sets it before GTK
// initialises (Wails' gtk3 init forces x11 when XDG_SESSION_TYPE is unset or x11, and its
// AppImage AppRun defaults to x11, so Wayland-native must set it explicitly).
func GDKBackendFor(kind platform.BackendKind) string {
	if kind == platform.BackendWayland {
		return "wayland"
	}
	return "x11"
}

// Anchor is where a layer surface sits: against the right (or left) output edge, Top and Side
// pixels in from the top and that edge.
type Anchor struct {
	Right     bool
	Top, Side int32
}

// AnchorFor turns a panel rect inside area (the output) into a layer-shell anchor: layer
// surfaces ignore positions, so the panel is pinned to the nearer vertical edge instead.
func AnchorFor(rect, area platform.Rect) Anchor {
	x, w := rect.X, int32(rect.Width)
	right := x > area.X+int32(area.Width)/2
	side := x - area.X
	if right {
		side = area.X + int32(area.Width) - (x + w)
	}
	return Anchor{Right: right, Top: rect.Y - area.Y, Side: max(side, 0)}
}
