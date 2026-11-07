// Package platform is RKL's OS integration behind interfaces.
//
// Rules:
//   - Exactly one global input hook per process. Hook callbacks only push into a channel.
//   - Missing permissions are reported through [Capabilities], never by panicking or spinning.
//   - Backends live in sub-packages; [Detect] chooses one at runtime.
package platform

import "unsafe"

// WindowTracker tracks the PoE2 game window.
type WindowTracker interface {
	// GameWindow is the current game window if found (title "Path of Exile 2").
	GameWindow() (WindowInfo, bool)
	// IsGameFocused reports whether the game window is focused right now.
	IsGameFocused() bool
	// Subscribe starts emitting window events; it may be called once.
	Subscribe() (<-chan WindowEvent, error)
}

// Binding pairs a hotkey action with its chord.
type Binding struct {
	Action HotkeyAction
	Chord  Chord
}

// HotkeyHost registers global hotkeys.
type HotkeyHost interface {
	Register(action HotkeyAction, chord Chord) error
	// RegisterAll registers several hotkeys at once (the Wayland portal confirms them
	// together). It stops at the first error.
	RegisterAll(bindings []Binding) error
	Unregister(action HotkeyAction) error
	UnregisterAll() error
	// Events is the stream of fired hotkeys; it may be called once.
	Events() (<-chan HotkeyEvent, error)
	// Notes are problems found after registration returned (a portal bind that failed later),
	// shown next to [Capabilities.Notes].
	Notes() []string
}

// RegisterEach is the plain RegisterAll for hosts where each binding is cheap.
func RegisterEach(h HotkeyHost, bindings []Binding) error {
	for _, b := range bindings {
		if err := h.Register(b.Action, b.Chord); err != nil {
			return err
		}
	}
	return nil
}

// InputListener is the passive keyboard (and optionally mouse wheel) hook, used for Esc to
// close and Ctrl+wheel stash tab switching.
type InputListener interface {
	// Start starts the single global hook. A second call returns ErrAlreadyStarted.
	Start() (<-chan InputEvent, error)
}

// InputSimulator sends the copy keystroke (and other chords) to the game.
type InputSimulator interface {
	SendChord(chord Chord) error
}

// Clipboard is the system clipboard, text only.
type Clipboard interface {
	// Text returns the clipboard text; ok is false when it holds no text.
	Text() (text string, ok bool, err error)
	SetText(text string) error
	Clear() error
	// HasNonText reports whether the clipboard holds something that is not text (an image),
	// which Text reports as missing and which SetText or Clear would destroy. Backends that
	// cannot tell cheaply return false.
	HasNonText() bool
}

// OverlayWindow is native styling for the overlay window, implemented over a native window
// handle by each backend.
type OverlayWindow interface {
	// Prepare applies one-time native styles (no-activate, always on top, all Spaces,
	// layer shell...).
	Prepare() error
	ShowAt(rect Rect) error
	Hide() error
	SetClickThrough(enabled bool) error
	// RaiseAbove stacks the overlay directly above owner (the game window's NativeID) and keeps
	// it there, where the WM puts a focused fullscreen game above "always on top" windows.
	RaiseAbove(owner *uint64) error
	// Activate gives the overlay keyboard focus, bypassing focus-stealing prevention where the
	// platform allows it (pinned panel, settings).
	Activate() error
}

// Platform is everything a backend provides, plus what it can actually do here.
type Platform struct {
	Kind           BackendKind
	Capabilities   Capabilities
	WindowTracker  WindowTracker
	Hotkeys        HotkeyHost
	InputListener  InputListener
	InputSimulator InputSimulator
	Clipboard      Clipboard
}

// Parts is what an OS backend package provides. The shell builds the [Platform] from it and
// adds what Wails already does on every OS: global hotkeys (Win32 RegisterHotKey, Carbon, X11
// grabs, the XDG GlobalShortcuts portal on Wayland) and the clipboard. So Parts sets every
// capability except GlobalHotkeys and Clipboard, which the shell fills in.
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
}

// Optional extras an [OverlayWindow] may implement; the shell checks with a type assertion and
// skips what a backend does not offer.
type (
	// Rounder gives the window rounded corners (physical px); width 0 removes the shape.
	Rounder interface {
		SetRoundedShape(width, height, radius int32) error
	}
	// DirectMover moves the window straight through the window system, bypassing the
	// toolkit's "already there" shortcut (X11).
	DirectMover interface {
		MoveDirect(x, y int32) error
	}
	// LayerAnchorer anchors a layer-shell surface to an output edge (Wayland-native): right
	// selects the right edge, top and side are margins in px.
	LayerAnchorer interface {
		AnchorLayer(right bool, top, side int32) error
	}
)
