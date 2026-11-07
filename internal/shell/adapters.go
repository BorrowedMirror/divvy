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

// NewHotkeys returns a hotkey host on app's global shortcut manager. x11 is true when RKL uses
// its X11 backend on Linux: Wails picks X11 key grabs or the Wayland GlobalShortcuts portal from
// the session type, but under XWayland the game (Proton/Wine) is an X11 window, so the keys must
// be grabbed through X11 as the Tauri build did.
func NewHotkeys(a *application.App, x11 bool) *Hotkeys {
	return &Hotkeys{app: a, x11: x11, bound: map[platform.HotkeyAction]string{}, events: make(chan platform.HotkeyEvent, 64)}
}

// register is GlobalShortcut.Register. Wails creates its shortcut backend on the first call and
// decides X11 or portal from XDG_SESSION_TYPE then, so with x11 set that one call runs with the
// variable set to x11; it is restored straight after, so programs RKL launches (the browser)
// still see the real session.
func (h *Hotkeys) register(accel string, callback func()) error {
	var err error
	first := false
	h.started.Do(func() {
		first = true
		if !h.x11 {
			err = h.app.GlobalShortcut.Register(accel, callback)
			return
		}
		prev, had := os.LookupEnv("XDG_SESSION_TYPE")
		_ = os.Setenv("XDG_SESSION_TYPE", "x11")
		err = h.app.GlobalShortcut.Register(accel, callback)
		if had {
			_ = os.Setenv("XDG_SESSION_TYPE", prev)
		} else {
			_ = os.Unsetenv("XDG_SESSION_TYPE")
		}
	})
	if first {
		return err
	}
	return h.app.GlobalShortcut.Register(accel, callback)
}

var _ platform.HotkeyHost = (*Hotkeys)(nil)

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
		if strings.Contains(err.Error(), "already registered") {
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
			first = platform.Backend("unregister %s: %v", accel, err)
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

// Clipboard implements platform.Clipboard with Wails' clipboard (it runs on the UI thread).
type Clipboard struct{ app *application.App }

// NewClipboard returns a clipboard on app.
func NewClipboard(a *application.App) Clipboard { return Clipboard{app: a} }

var _ platform.Clipboard = Clipboard{}

// Text returns the clipboard text; ok is false when it holds none.
func (c Clipboard) Text() (string, bool, error) {
	text, ok := c.app.Clipboard.Text()
	if !ok || text == "" {
		return "", false, nil
	}
	return text, true, nil
}

// SetText replaces the clipboard with text.
func (c Clipboard) SetText(text string) error {
	if !c.app.Clipboard.SetText(text) {
		return platform.Backend("clipboard: could not set text")
	}
	return nil
}

// Clear empties the clipboard.
func (c Clipboard) Clear() error { return c.SetText("") }

// HasNonText is false: Wails cannot tell whether the clipboard holds an image.
func (c Clipboard) HasNonText() bool { return false }
