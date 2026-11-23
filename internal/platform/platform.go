package platform

import (
	"context"
	"unsafe"
)

// Parts is what an OS backend package provides. The shell completes it into a [Platform] with
// what Wails already does on every OS: global hotkeys (Win32 RegisterHotKey, Carbon, X11 grabs,
// the XDG GlobalShortcuts portal on Wayland) and the clipboard. So Parts sets every capability
// except GlobalHotkeys and Clipboard, which the shell fills in.
type Parts struct {
	Kind           BackendKind
	Capabilities   Capabilities
	WindowTracker  WindowTracker
	InputListener  InputListener
	InputSimulator InputSimulator
	// Overlay styles a Wails window given its NativeWindow() pointer: an HWND on Windows, an
	// NSWindow* on macOS, a GtkWindow* on Linux.
	Overlay func(native unsafe.Pointer) (OverlayWindow, error)
	// Cursor is the mouse position in physical screen pixels (macOS: points); ok is false when
	// the backend cannot tell (Wayland-native). Wails has no global cursor query.
	Cursor func() (x, y float64, ok bool)
	// ActivateWindow gives keyboard focus to another window by its native id (the game, after
	// the panel closes). Nil where the platform cannot.
	ActivateWindow func(nativeID uint64) error
	// Clipboard is the clipboard: the backend's own where Wails' does not work (Wayland-native:
	// GTK only sees clipboard changes while Divvy has a focused window, so the backend reads it
	// through data-control), else nil until the shell sets Wails'.
	Clipboard Clipboard
}

// Platform is everything the app uses: a backend's [Parts] plus the hotkey host.
type Platform struct {
	Parts
	Hotkeys HotkeyHost
}

// Unavailable stands in for a [WindowTracker], [InputListener] or [InputSimulator] a backend
// cannot provide here; the string names the capability, which the backend reports as false.
// Every stream and send fails with [ErrUnsupported].
type Unavailable string

var (
	_ WindowTracker  = Unavailable("")
	_ InputListener  = Unavailable("")
	_ InputSimulator = Unavailable("")
)

// GameWindow is never found.
func (Unavailable) GameWindow() (WindowInfo, bool) { return WindowInfo{}, false }

// IsGameFocused is true: without tracking nothing can tell, and hotkeys must keep working.
func (Unavailable) IsGameFocused() bool { return true }

// Subscribe fails with [ErrUnsupported].
func (u Unavailable) Subscribe(context.Context) (<-chan WindowEvent, error) {
	return nil, Unsupported(string(u))
}

// Start fails with [ErrUnsupported].
func (u Unavailable) Start(context.Context) (<-chan InputEvent, error) {
	return nil, Unsupported(string(u))
}

// SendChord fails with [ErrUnsupported].
func (u Unavailable) SendChord(Chord) error { return Unsupported(string(u)) }
