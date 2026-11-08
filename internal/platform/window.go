package platform

import (
	"context"
	"encoding/json"
	"fmt"
)

// GameWindowTitle is the window title we track.
const GameWindowTitle = "Path of Exile 2"

// Rect is a window rectangle in screen pixels.
type Rect struct {
	X      int32  `json:"x"`
	Y      int32  `json:"y"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

// WindowInfo describes the game window.
type WindowInfo struct {
	Title   string `json:"title"`
	Rect    Rect   `json:"rect"`
	Focused bool   `json:"focused"`
	// NativeID is the OS-specific id (HWND, CGWindowID, X11 window id).
	NativeID uint64 `json:"native_id"`
}

// WindowEventType says which [WindowEvent] fields are set.
type WindowEventType string

// Window event types.
const (
	WindowFound        WindowEventType = "found"
	WindowMoved        WindowEventType = "moved"
	WindowFocusChanged WindowEventType = "focus_changed"
	WindowLost         WindowEventType = "lost"
)

// WindowEvent is a change to the game window. Found carries Info, Moved carries Rect,
// FocusChanged carries Focused, Lost carries nothing.
type WindowEvent struct {
	Type    WindowEventType
	Info    WindowInfo
	Rect    Rect
	Focused bool
}

// MarshalJSON writes the event as an object tagged by `type`, with only the fields of that
// type: `{"type": "found", "title": ..., "rect": ..., ...}`, `{"type": "lost"}`.
func (e WindowEvent) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case WindowFound:
		return json.Marshal(struct {
			Type WindowEventType `json:"type"`
			WindowInfo
		}{e.Type, e.Info})
	case WindowMoved:
		return json.Marshal(struct {
			Type WindowEventType `json:"type"`
			Rect Rect            `json:"rect"`
		}{e.Type, e.Rect})
	case WindowFocusChanged:
		return json.Marshal(struct {
			Type    WindowEventType `json:"type"`
			Focused bool            `json:"focused"`
		}{e.Type, e.Focused})
	case WindowLost:
		return json.Marshal(struct {
			Type WindowEventType `json:"type"`
		}{e.Type})
	}
	return nil, fmt.Errorf("unknown window event type %q", e.Type)
}

// UnmarshalJSON reads the form [WindowEvent.MarshalJSON] writes.
func (e *WindowEvent) UnmarshalJSON(b []byte) error {
	var raw struct {
		Type WindowEventType `json:"type"`
		WindowInfo
		Focused bool `json:"focused"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*e = WindowEvent{Type: raw.Type}
	switch raw.Type {
	case WindowFound:
		e.Info = raw.WindowInfo
	case WindowMoved:
		e.Rect = raw.Rect
	case WindowFocusChanged:
		e.Focused = raw.Focused
	case WindowLost:
	default:
		return fmt.Errorf("unknown window event type %q", raw.Type)
	}
	return nil
}

// WindowTracker tracks the PoE2 game window.
type WindowTracker interface {
	// GameWindow is the current game window if found (title "Path of Exile 2").
	GameWindow() (WindowInfo, bool)
	// IsGameFocused reports whether the game window is focused right now.
	IsGameFocused() bool
	// Subscribe starts emitting window events; it may be called once. The tracker stops, and
	// closes the channel, when ctx is done or it can no longer track (the display went away).
	Subscribe(ctx context.Context) (<-chan WindowEvent, error)
}

// DiffWindows returns the events that move a subscriber from prev to next (nil = no game
// window). A window with another NativeID (the game restarted) is a single Found, without a
// Lost first: the subscriber replaces the window it follows, and whatever it shows over the
// game (the settings overlay) moves to the new window instead of closing.
func DiffWindows(prev, next *WindowInfo) []WindowEvent {
	switch {
	case prev == nil && next == nil:
		return nil
	case next == nil:
		return []WindowEvent{{Type: WindowLost}}
	case prev == nil || prev.NativeID != next.NativeID:
		return []WindowEvent{{Type: WindowFound, Info: *next}}
	}
	var out []WindowEvent
	if prev.Rect != next.Rect {
		out = append(out, WindowEvent{Type: WindowMoved, Rect: next.Rect})
	}
	if prev.Focused != next.Focused {
		out = append(out, WindowEvent{Type: WindowFocusChanged, Focused: next.Focused})
	}
	return out
}
