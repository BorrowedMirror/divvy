package wayland

import (
	"fmt"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// Capability notes, shown on the diagnostics page.
const (
	noteNoTracking = "Wayland-native: game window cannot be tracked; overlay anchored to the output"
	noteNoHook     = "Wayland-native: no passive input hook"
	noteGNOME      = "GNOME has no layer-shell: the overlay cannot stay above the game; use X11/XWayland"
	noteNoPortal   = "No GlobalShortcuts portal: bind `divvy --trigger` (or `pkill -USR2 divvy`; `-USR1` to pin) in " +
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
		Notes:               []string{noteNoTracking, noteNoHook},
	}
	if gnome {
		caps.Notes = append(caps.Notes, noteGNOME)
	}
	return caps
}

// hotkeyNotes are the notes for the shell's GlobalHotkeys capability: the signal fallback when
// the GlobalShortcuts portal is missing.
func hotkeyNotes(portal bool) []string {
	if portal {
		return nil
	}
	return []string{noteNoPortal}
}

// virtualKeyboardPlausible reports whether the compositor may implement
// zwp_virtual_keyboard_v1 (wlroots-based ones do; mutter and KWin do not).
func virtualKeyboardPlausible(probe platform.EnvProbe) bool {
	return !probe.IsGNOME() && !probe.HasDesktop("kde")
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
