package platform

import (
	"encoding/json"
	"fmt"
)

// HotkeyKind is the kind of a [HotkeyAction].
type HotkeyKind string

// Hotkey kinds.
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

// MarshalJSON writes a plain action as its kind (`"price_check"`) and an indexed one as a
// one-key object (`{"chat": 2}`).
func (a HotkeyAction) MarshalJSON() ([]byte, error) {
	if a.indexed() {
		return json.Marshal(map[string]uint8{string(a.Kind): a.Index})
	}
	return json.Marshal(string(a.Kind))
}

// UnmarshalJSON reads the form [HotkeyAction.MarshalJSON] writes.
func (a *HotkeyAction) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		switch k := HotkeyKind(s); k {
		case PriceCheck, PriceCheckPinned, OpenSettings, OpenTrade:
			*a = HotkeyAction{Kind: k}
			return nil
		case Chat, StashSearch:
			return fmt.Errorf("hotkey action %q needs an index", s)
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
