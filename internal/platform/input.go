package platform

import (
	"context"
	"encoding/json"
	"fmt"
)

// InputEventType says which [InputEvent] fields are set.
type InputEventType string

// Input event types.
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

// MarshalJSON writes the event as an object tagged by `type`, with only the fields of that
// type: `{"type": "key_down", "key": "Escape"}`.
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

// InputListener is the passive keyboard (and optionally mouse wheel) hook, used for Esc to
// close and Ctrl+wheel stash tab switching.
type InputListener interface {
	// Start starts the single global hook. A second call while a hook runs returns
	// [ErrAlreadyStarted]. The hook is removed, and the channel closed, when ctx is done or the
	// hook dies; Start may then be called again.
	Start(ctx context.Context) (<-chan InputEvent, error)
}

// InputSimulator sends the copy keystroke (and other chords) to the game.
type InputSimulator interface {
	SendChord(chord Chord) error
}

// InputBufferSize is how many events a passive hook buffers for a slow consumer before it drops
// new ones.
const InputBufferSize = 1024

// TrySend sends v on ch without blocking and reports whether it did. Hook callbacks hand events
// off with it: they run on the OS input path and must never wait for a consumer.
func TrySend[T any](ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	default:
		return false
	}
}
