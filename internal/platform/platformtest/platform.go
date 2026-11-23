package platformtest

import (
	"reflect"
	"time"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// Capabilities are the capabilities the fake backend reports: everything works.
func Capabilities() platform.Capabilities {
	return platform.Capabilities{
		WindowTracking:      true,
		OverlayAlwaysOnTop:  true,
		OverlayClickThrough: true,
		GlobalHotkeys:       true,
		PassiveHook:         true,
		KeystrokeInjection:  true,
		Clipboard:           true,
		Notes:               []string{"fake backend (platformtest)"},
	}
}

// Options configures [New]. The zero value is an empty clipboard, no game window and a game
// that copies nothing.
type Options struct {
	// ItemText is what the game copies on any chord ("" copies nothing).
	ItemText string
	// AdvancedText is what the game copies on Ctrl+Alt+C (advanced copy) instead of ItemText.
	AdvancedText string
	// CopyDelay is the delay between a chord and the clipboard write.
	CopyDelay time.Duration
	// Window is the game window the tracker reports (zero: none).
	Window platform.WindowInfo
	// Clipboard is the initial clipboard text (to test that it is restored).
	Clipboard string
	// Caps are the capabilities reported (zero: [Capabilities]).
	Caps platform.Capabilities
}

// Handles are the test's handles to the fakes inside a platform built by [New]: the same
// pointers the platform holds.
type Handles struct {
	Clipboard      *Clipboard
	InputSimulator *InputSimulator
	WindowTracker  *WindowTracker
	Hotkeys        *HotkeyHost
	InputListener  *InputListener
}

// New builds a platform of fakes and the handles to drive them.
func New(opts Options) (platform.Platform, Handles) {
	h := Handles{
		Clipboard:     NewClipboard(opts.Clipboard),
		WindowTracker: NewWindowTracker(),
		Hotkeys:       NewHotkeyHost(),
		InputListener: NewInputListener(),
	}
	h.InputSimulator = NewInputSimulator(h.Clipboard)
	h.InputSimulator.SetDelay(opts.CopyDelay)
	h.InputSimulator.Respond(opts.ItemText)
	if opts.AdvancedText != "" {
		h.InputSimulator.OnChord(platform.CopyChord(true), opts.AdvancedText)
	}
	if opts.Window != (platform.WindowInfo{}) {
		w := opts.Window
		h.WindowTracker.window = &w // already there: no WindowFound event
	}
	caps := opts.Caps
	if reflect.ValueOf(caps).IsZero() {
		caps = Capabilities()
	}
	p := platform.Platform{
		Kind:           platform.BackendFake,
		Capabilities:   caps,
		WindowTracker:  h.WindowTracker,
		Hotkeys:        h.Hotkeys,
		InputListener:  h.InputListener,
		InputSimulator: h.InputSimulator,
		Clipboard:      h.Clipboard,
	}
	return p, h
}
