package shell

import (
	"os"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Hotkeys implements platform.HotkeyHost with Wails' global shortcuts (Win32 RegisterHotKey,
// Carbon hot keys, X11 grabs, the XDG GlobalShortcuts portal on Wayland).
type Hotkeys struct {
	app *application.App
	// x11 makes Wails grab the keys through X11 even in a Wayland session (see NewHotkeys).
	x11     bool
	started sync.Once
	mu      sync.Mutex
	bound   map[platform.HotkeyAction]string // action -> accelerator
	events  chan platform.HotkeyEvent
	taken   bool
}

var _ platform.HotkeyHost = (*Hotkeys)(nil)

// NewHotkeys returns a hotkey host on app's global shortcut manager. x11 is true when RKL uses
// its X11 backend on Linux: Wails picks X11 key grabs or the Wayland GlobalShortcuts portal from
// the session type, but under XWayland the game (Proton/Wine) is an X11 window, so the keys must
// be grabbed through X11.
func NewHotkeys(a *application.App, x11 bool) *Hotkeys {
	return &Hotkeys{app: a, x11: x11, bound: map[platform.HotkeyAction]string{}, events: make(chan platform.HotkeyEvent, 64)}
}

// register is GlobalShortcut.Register. Wails creates its shortcut backend on the first call,
// choosing X11 grabs or the portal from XDG_SESSION_TYPE, so with x11 set that one call runs
// with the variable set to x11.
//
// The override is scoped to that call rather than set for the whole process before Wails
// starts: Wails also reads the session type to set up GTK and the GPU, and programs RKL
// launches (the browser) inherit the environment; all of them must see the real session.
func (h *Hotkeys) register(accel string, callback func()) error {
	var err error
	first := false
	h.started.Do(func() {
		first = true
		if h.x11 {
			restore := setEnv("XDG_SESSION_TYPE", "x11")
			defer restore()
		}
		err = h.app.GlobalShortcut.Register(accel, callback)
	})
	if first {
		return err
	}
	return h.app.GlobalShortcut.Register(accel, callback)
}

// setEnv sets an environment variable and returns a function that restores its previous
// value (or unsets it).
func setEnv(key, value string) (restore func()) {
	prev, had := os.LookupEnv(key)
	_ = os.Setenv(key, value)
	return func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

// isTaken reports whether a Wails registration error means another application, or another
// action, already has the chord. Wails returns these as plain errors on every OS ("the
// shortcut is already registered (possibly by another application)", "global shortcut %q is
// already registered"), so the text is all there is to go on.
func isTaken(err error) bool {
	return strings.Contains(err.Error(), "already registered")
}

// Register binds chord to action, replacing the action's previous chord.
func (h *Hotkeys) Register(action platform.HotkeyAction, chord platform.Chord) error {
	accel, err := platform.Accelerator(chord)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.bound[action]; ok {
		if old == accel {
			return nil
		}
		_ = h.app.GlobalShortcut.Unregister(old)
		delete(h.bound, action)
	}
	for _, other := range h.bound {
		if other == accel {
			return platform.HotkeyConflict(chord.String())
		}
	}
	if err := h.register(accel, func() {
		select {
		case h.events <- platform.HotkeyEvent{Action: action}:
		default: // the consumer is stuck; drop the press rather than block the UI thread
		}
	}); err != nil {
		if isTaken(err) {
			return platform.HotkeyConflict(chord.String())
		}
		return platform.InvalidChord(chord.String() + ": " + err.Error())
	}
	h.bound[action] = accel
	return nil
}

// RegisterAll binds every binding, stopping at the first error.
func (h *Hotkeys) RegisterAll(bindings []platform.Binding) error {
	return platform.RegisterEach(h, bindings)
}

// Unregister releases one action's chord.
func (h *Hotkeys) Unregister(action platform.HotkeyAction) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	accel, ok := h.bound[action]
	if !ok {
		return nil
	}
	delete(h.bound, action)
	return h.app.GlobalShortcut.Unregister(accel)
}

// UnregisterAll releases every chord.
func (h *Hotkeys) UnregisterAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var first error
	for action, accel := range h.bound {
		if err := h.app.GlobalShortcut.Unregister(accel); err != nil && first == nil {
			first = platform.Backend("unregister %s: %w", accel, err)
		}
		delete(h.bound, action)
	}
	return first
}

// Events is the stream of fired hotkeys; it may be taken once.
func (h *Hotkeys) Events() (<-chan platform.HotkeyEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.taken {
		return nil, platform.ErrAlreadyStarted
	}
	h.taken = true
	return h.events, nil
}

// Notes has nothing to add: Wails reports registration failures from Register itself.
func (h *Hotkeys) Notes() []string { return nil }
