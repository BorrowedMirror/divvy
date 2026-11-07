//go:build linux

package wayland

import (
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Option configures [New].
type Option func(*options)

type options struct {
	probe      *platform.EnvProbe
	mainThread func(func())
	display    string // virtual keyboard display; "" = $WAYLAND_DISPLAY
	noYdotool  bool
}

// WithProbe uses probe instead of the process environment.
func WithProbe(probe platform.EnvProbe) Option {
	return func(o *options) { o.probe = &probe }
}

// WithMainThread runs the overlay's GTK calls through run; pass Wails' application.InvokeSync.
// Without it the overlay methods must be called on the GTK main thread.
func WithMainThread(run func(func())) Option {
	return func(o *options) { o.mainThread = run }
}

// New builds the Wayland-native backend. It fails without WAYLAND_DISPLAY.
//
// GlobalHotkeys and Clipboard are left to the shell (Wails); see [GlobalHotkeys] and
// [HotkeyNotes] for the hotkey capability and [SignalTriggers] for the signal fallback.
// Cursor and ActivateWindow stay nil: a Wayland client can neither read the global cursor nor
// focus another client's window.
func New(opts ...Option) (platform.Parts, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	probe := platform.EnvProbeFromEnv()
	if o.probe != nil {
		probe = *o.probe
	}
	if probe.WaylandDisplay == "" {
		return platform.Parts{}, platform.Unsupported("wayland backend needs WAYLAND_DISPLAY")
	}
	run := o.mainThread
	if run == nil {
		run = func(f func()) { f() }
	}
	gnome := probe.IsGNOME()

	caps := capabilities(probe)
	vk := &virtualKeyboard{display: o.display}
	eager := vk.ensureReady()
	var yd *ydotool
	if !o.noYdotool {
		yd, _ = detectYdotool()
	}
	plan := injectionPlan(eager, yd != nil, virtualKeyboardPlausible(probe))
	caps.KeystrokeInjection = plan.available
	if plan.note != "" {
		caps.Notes = append(caps.Notes, plan.note)
	}
	var sim platform.InputSimulator = unsupported("keystroke_injection")
	if plan.available {
		if yd != nil {
			vk.fallback = yd
		}
		sim = vk
	}

	return platform.Parts{
		Kind:           platform.BackendWayland,
		Capabilities:   caps,
		WindowTracker:  unsupported("window_tracking"),
		InputListener:  unsupported("passive_hook"),
		InputSimulator: sim,
		Overlay: func(native unsafe.Pointer) (platform.OverlayWindow, error) {
			return newOverlay(native, run, gnome)
		},
	}, nil
}

// unsupported stands in for a capability Wayland-native cannot provide; every call fails with
// [platform.ErrUnsupported] (the matching capability is false).
type unsupported string

// GameWindow implements [platform.WindowTracker]: never found.
func (unsupported) GameWindow() (platform.WindowInfo, bool) { return platform.WindowInfo{}, false }

// IsGameFocused implements [platform.WindowTracker]. Without tracking we cannot tell; assume
// focused so hotkeys still work.
func (unsupported) IsGameFocused() bool { return true }

// Subscribe implements [platform.WindowTracker].
func (u unsupported) Subscribe() (<-chan platform.WindowEvent, error) {
	return nil, platform.Unsupported(string(u))
}

// Start implements [platform.InputListener].
func (u unsupported) Start() (<-chan platform.InputEvent, error) {
	return nil, platform.Unsupported(string(u))
}

// SendChord implements [platform.InputSimulator].
func (u unsupported) SendChord(platform.Chord) error { return platform.Unsupported(string(u)) }
