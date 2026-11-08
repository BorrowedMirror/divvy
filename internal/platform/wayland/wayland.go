//go:build linux

package wayland

import (
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Options configures [New].
type Options struct {
	// Probe is the environment the backend runs in ([platform.EnvProbeFromEnv]).
	Probe platform.EnvProbe
	// MainThread runs the overlay's GTK calls; pass Wails' application.InvokeSync. Without it
	// the overlay methods must be called on the GTK main thread.
	MainThread func(func())

	// display is the virtual keyboard's Wayland display ("" = $WAYLAND_DISPLAY) and noYdotool
	// turns the ydotool fallback off; both for tests.
	display   string
	noYdotool bool
}

// New builds the Wayland-native backend. It fails without WAYLAND_DISPLAY in opts.Probe.
//
// GlobalHotkeys and Clipboard are left to the shell (Wails); see [GlobalHotkeys] for the
// hotkey capability and [platform.SignalTriggers] for the signal fallback. Cursor and
// ActivateWindow stay nil: a Wayland client can neither read the global cursor nor focus
// another client's window.
func New(opts Options) (platform.Parts, error) {
	probe := opts.Probe
	if probe.WaylandDisplay == "" {
		return platform.Parts{}, platform.Unsupported("wayland backend needs WAYLAND_DISPLAY")
	}
	run := opts.MainThread
	if run == nil {
		run = func(f func()) { f() }
	}
	gnome := probe.IsGNOME()

	caps := capabilities(probe)
	vk := &virtualKeyboard{display: opts.display}
	eager := vk.ensureReady()
	var yd *ydotool
	if !opts.noYdotool {
		yd = detectYdotool()
	}
	plan := injectionPlan(eager, yd != nil, virtualKeyboardPlausible(probe))
	caps.KeystrokeInjection = plan.available
	if plan.note != "" {
		caps.Notes = append(caps.Notes, plan.note)
	}
	var sim platform.InputSimulator = platform.Unavailable("keystroke_injection")
	if plan.available {
		if yd != nil {
			vk.fallback = yd
		}
		sim = vk
	}

	parts := platform.Parts{
		Kind:           platform.BackendWayland,
		Capabilities:   caps,
		WindowTracker:  platform.Unavailable("window_tracking"),
		InputListener:  platform.Unavailable("passive_hook"),
		InputSimulator: sim,
		Overlay: func(native unsafe.Pointer) (platform.OverlayWindow, error) {
			return newOverlay(native, run, gnome)
		},
	}
	if clip := detectWLClipboard(); clip != nil {
		parts.Clipboard = clip
	} else {
		parts.Capabilities.Notes = append(parts.Capabilities.Notes, "install wl-clipboard (wl-paste, wl-copy): without it the item the game copies is only seen while an RKL window has focus")
	}
	return parts, nil
}
