package platform

import (
	"encoding/json"
	"fmt"
	"strings"
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

// MarshalJSON writes serde's internally tagged form (`{"type": "found", ...}`).
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

// UnmarshalJSON reads serde's internally tagged form.
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

// KeyModifier is a chord modifier.
type KeyModifier string

const (
	Ctrl  KeyModifier = "ctrl"
	Alt   KeyModifier = "alt"
	Shift KeyModifier = "shift"
	Meta  KeyModifier = "meta"
)

// Chord is a key chord such as `Ctrl+Alt+D`. Key uses W3C `KeyboardEvent.code` names
// (`KeyD`, `F5`).
type Chord struct {
	Modifiers []KeyModifier `json:"modifiers"`
	Key       string        `json:"key"`
}

// NewChord builds a chord.
func NewChord(key string, modifiers ...KeyModifier) Chord {
	return Chord{Modifiers: append([]KeyModifier{}, modifiers...), Key: key}
}

// UnboundChord is a hotkey the user has explicitly cleared ("Not set"). It is stored as an
// empty key so it is saved and never silently replaced by the default.
func UnboundChord() Chord {
	return Chord{Modifiers: []KeyModifier{}, Key: ""}
}

// IsUnbound reports whether the chord has no key (see [UnboundChord]).
func (c Chord) IsUnbound() bool {
	return strings.TrimSpace(c.Key) == ""
}

// Equal compares two chords, modifier order included.
func (c Chord) Equal(o Chord) bool {
	if c.Key != o.Key || len(c.Modifiers) != len(o.Modifiers) {
		return false
	}
	for i := range c.Modifiers {
		if c.Modifiers[i] != o.Modifiers[i] {
			return false
		}
	}
	return true
}

// ParseChord parses `Ctrl+Alt+D` / `ctrl+shift+F5` style strings.
func ParseChord(s string) (Chord, error) {
	mods := []KeyModifier{}
	key := ""
	haveKey := false
	for raw := range strings.SplitSeq(s, "+") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		switch strings.ToLower(part) {
		case "ctrl", "control":
			mods = append(mods, Ctrl)
		case "alt", "option":
			mods = append(mods, Alt)
		case "shift":
			mods = append(mods, Shift)
		case "meta", "cmd", "super", "win":
			mods = append(mods, Meta)
		default:
			if haveKey {
				return Chord{}, InvalidChord(s)
			}
			haveKey = true
			lower := strings.ToLower(part)
			switch {
			case len(lower) == 1 && lower[0] >= 'a' && lower[0] <= 'z':
				key = "Key" + strings.ToUpper(lower)
			case len(lower) == 1 && lower[0] >= '0' && lower[0] <= '9':
				key = "Digit" + lower
			default:
				key = part
			}
		}
	}
	if !haveKey {
		return Chord{}, InvalidChord(s)
	}
	return Chord{Modifiers: mods, Key: key}, nil
}

// String formats the chord as `Ctrl+Alt+D`.
func (c Chord) String() string {
	var b strings.Builder
	for _, m := range c.Modifiers {
		switch m {
		case Ctrl:
			b.WriteString("Ctrl+")
		case Alt:
			b.WriteString("Alt+")
		case Shift:
			b.WriteString("Shift+")
		case Meta:
			b.WriteString("Meta+")
		}
	}
	k := c.Key
	if rest, ok := strings.CutPrefix(k, "Key"); ok {
		k = rest
	} else if rest, ok := strings.CutPrefix(k, "Digit"); ok {
		k = rest
	}
	b.WriteString(k)
	return b.String()
}

// HotkeyKind is the kind of a [HotkeyAction].
type HotkeyKind string

const (
	PriceCheck       HotkeyKind = "price_check"
	PriceCheckPinned HotkeyKind = "price_check_pinned"
	OpenSettings     HotkeyKind = "open_settings"
	OpenTrade        HotkeyKind = "open_trade"
	// Chat is a chat command hotkey; Index points into the configured chat commands.
	Chat HotkeyKind = "chat"
	// StashSearch is a stash search hotkey; Index points into the configured stash searches.
	StashSearch HotkeyKind = "stash_search"
)

// HotkeyAction is what a hotkey does. It is comparable, so it can key a map.
type HotkeyAction struct {
	Kind  HotkeyKind
	Index uint8 // Chat and StashSearch only
}

// Action returns the action for a kind without an index.
func Action(kind HotkeyKind) HotkeyAction { return HotkeyAction{Kind: kind} }

// ChatAction returns the action for chat command i.
func ChatAction(i uint8) HotkeyAction { return HotkeyAction{Kind: Chat, Index: i} }

// StashSearchAction returns the action for stash search i.
func StashSearchAction(i uint8) HotkeyAction { return HotkeyAction{Kind: StashSearch, Index: i} }

func (a HotkeyAction) indexed() bool { return a.Kind == Chat || a.Kind == StashSearch }

// MarshalJSON writes serde's externally tagged form: `"price_check"` or `{"chat": 2}`.
func (a HotkeyAction) MarshalJSON() ([]byte, error) {
	if a.indexed() {
		return json.Marshal(map[string]uint8{string(a.Kind): a.Index})
	}
	return json.Marshal(string(a.Kind))
}

// UnmarshalJSON reads serde's externally tagged form.
func (a *HotkeyAction) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		switch k := HotkeyKind(s); k {
		case PriceCheck, PriceCheckPinned, OpenSettings, OpenTrade:
			*a = HotkeyAction{Kind: k}
			return nil
		}
		return fmt.Errorf("unknown hotkey action %q", s)
	}
	var m map[string]uint8
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if len(m) == 1 {
		for k, i := range m {
			if kind := HotkeyKind(k); kind == Chat || kind == StashSearch {
				*a = HotkeyAction{Kind: kind, Index: i}
				return nil
			}
		}
	}
	return fmt.Errorf("unknown hotkey action %s", b)
}

// HotkeyEvent is a fired hotkey.
type HotkeyEvent struct {
	Action HotkeyAction `json:"action"`
}

// InputEventType says which [InputEvent] fields are set.
type InputEventType string

const (
	KeyDown   InputEventType = "key_down"
	KeyUp     InputEventType = "key_up"
	MouseMove InputEventType = "mouse_move"
	Wheel     InputEventType = "wheel"
)

// InputEvent is an event from the passive input hook. KeyDown/KeyUp carry Key (a W3C code
// name), MouseMove carries X and Y, Wheel carries Delta.
type InputEvent struct {
	Type  InputEventType
	Key   string
	X, Y  int32
	Delta int32
}

// MarshalJSON writes serde's internally tagged form.
func (e InputEvent) MarshalJSON() ([]byte, error) {
	type tag = InputEventType
	switch e.Type {
	case KeyDown, KeyUp:
		return json.Marshal(struct {
			Type tag    `json:"type"`
			Key  string `json:"key"`
		}{e.Type, e.Key})
	case MouseMove:
		return json.Marshal(struct {
			Type tag   `json:"type"`
			X    int32 `json:"x"`
			Y    int32 `json:"y"`
		}{e.Type, e.X, e.Y})
	case Wheel:
		return json.Marshal(struct {
			Type  tag   `json:"type"`
			Delta int32 `json:"delta"`
		}{e.Type, e.Delta})
	}
	return nil, fmt.Errorf("unknown input event type %q", e.Type)
}

// Capabilities is the per-capability support report shown on the diagnostics page.
type Capabilities struct {
	WindowTracking      bool `json:"window_tracking"`
	OverlayAlwaysOnTop  bool `json:"overlay_always_on_top"`
	OverlayClickThrough bool `json:"overlay_click_through"`
	GlobalHotkeys       bool `json:"global_hotkeys"`
	PassiveHook         bool `json:"passive_hook"`
	KeystrokeInjection  bool `json:"keystroke_injection"`
	Clipboard           bool `json:"clipboard"`
	// MissingPermissions lists permissions still missing (macOS: `accessibility`,
	// `screen_recording`).
	MissingPermissions []string `json:"missing_permissions"`
	// Notes are human-readable notes, e.g. "Wayland-native: overlay anchored to output".
	Notes []string `json:"notes"`
}

// MarshalJSON writes empty lists as `[]`, as serde does, never `null`.
func (c Capabilities) MarshalJSON() ([]byte, error) {
	type plain Capabilities
	if c.MissingPermissions == nil {
		c.MissingPermissions = []string{}
	}
	if c.Notes == nil {
		c.Notes = []string{}
	}
	return json.Marshal(plain(c))
}
