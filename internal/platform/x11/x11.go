//go:build linux

package x11

import (
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Options configures [New].
type Options struct {
	// Display is the X display; empty means $DISPLAY.
	Display string
	// HookMouse also records pointer motion, buttons and the wheel in the passive hook (Ctrl +
	// wheel stash tab switching). Off, only the keyboard is hooked, which keeps mouse latency
	// in the game untouched.
	HookMouse bool
}

// unsupported stands in for a piece the X server cannot provide.
type unsupported string

func (u unsupported) Start() (<-chan platform.InputEvent, error) {
	return nil, platform.Unsupported(string(u))
}

func (u unsupported) SendChord(platform.Chord) error { return platform.Unsupported(string(u)) }

// New connects to the X server and builds the backend. It fails only when there is no X server;
// pieces the server lacks degrade to Unsupported with their capability cleared and a note.
func New(opts Options) (platform.Parts, error) {
	probe := platform.EnvProbeFromEnv()
	x, err := connect(opts.Display, nil)
	if err != nil {
		return platform.Parts{}, err
	}
	caps := platform.Capabilities{
		WindowTracking:      true,
		OverlayAlwaysOnTop:  true,
		OverlayClickThrough: x.hasExtension("XFIXES") && x.hasExtension("SHAPE"),
	}
	parts := platform.Parts{
		Kind:          platform.BackendX11,
		WindowTracker: newTracker(x, opts.Display),
		Overlay: func(native unsafe.Pointer) (platform.OverlayWindow, error) {
			return overlayFromNative(opts.Display, native)
		},
		Cursor: func() (float64, float64, bool) { return Cursor(opts.Display) },
		ActivateWindow: func(id uint64) error {
			return ActivateWindow(opts.Display, id)
		},
	}

	if x.hasExtension("RECORD") {
		caps.PassiveHook = true
		parts.InputListener = NewListener(opts.Display, opts.HookMouse)
	} else {
		caps.Notes = append(caps.Notes, "X server lacks the RECORD extension: passive input hook disabled")
		parts.InputListener = unsupported("passive_hook")
	}

	if x.hasExtension("XTEST") {
		caps.KeystrokeInjection = true
		sim := NewSimulator(opts.Display)
		if err := sim.EnsureReady(); err != nil {
			caps.Notes = append(caps.Notes, "keystroke injection not ready at startup ("+err.Error()+"); retried on each copy")
		}
		parts.InputSimulator = sim
	} else {
		caps.Notes = append(caps.Notes, "X server lacks XTEST: cannot send Ctrl+C to the game")
		parts.InputSimulator = unsupported("keystroke_injection")
	}

	caps.Notes = append(caps.Notes, sessionNotes(probe)...)
	parts.Capabilities = caps
	return parts, nil
}
