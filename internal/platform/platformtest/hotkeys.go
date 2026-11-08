package platformtest

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// HotkeyHost is a hotkey host; the test presses a hotkey with Fire or Press.
type HotkeyHost struct {
	mu         sync.Mutex
	registered map[platform.HotkeyAction]platform.Chord
	// taken are chords "owned by another application": registering them fails.
	taken  []platform.Chord
	events stream[platform.HotkeyEvent]
}

var _ platform.HotkeyHost = (*HotkeyHost)(nil)

// NewHotkeyHost returns a host with nothing registered.
func NewHotkeyHost() *HotkeyHost {
	return &HotkeyHost{registered: map[platform.HotkeyAction]platform.Chord{}}
}

// Fire simulates the user pressing the hotkey for action. It fires even if action is not
// registered (tests of the consumer usually don't care) and reports whether it was delivered.
func (h *HotkeyHost) Fire(action platform.HotkeyAction) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events.emit(platform.HotkeyEvent{Action: action})
}

// Press simulates pressing chord: it fires the action bound to it, if any.
func (h *HotkeyHost) Press(chord platform.Chord) (platform.HotkeyAction, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for a, c := range h.registered {
		if c.Equal(chord) {
			h.events.emit(platform.HotkeyEvent{Action: a})
			return a, true
		}
	}
	return platform.HotkeyAction{}, false
}

// Block marks chord as grabbed by another application.
func (h *HotkeyHost) Block(chord platform.Chord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taken = append(h.taken, chord)
}

// Registered returns a copy of the current registrations.
func (h *HotkeyHost) Registered() map[platform.HotkeyAction]platform.Chord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.registered)
}

// Register implements [platform.HotkeyHost].
func (h *HotkeyHost) Register(action platform.HotkeyAction, chord platform.Chord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if slices.ContainsFunc(h.taken, chord.Equal) {
		return platform.HotkeyConflict(chord.String())
	}
	for a, c := range h.registered {
		if a != action && c.Equal(chord) {
			return platform.HotkeyConflict(chord.String())
		}
	}
	h.registered[action] = chord
	return nil
}

// RegisterAll implements [platform.HotkeyHost] one binding at a time.
func (h *HotkeyHost) RegisterAll(bindings []platform.Binding) error {
	return platform.RegisterEach(h, bindings)
}

// Unregister implements [platform.HotkeyHost].
func (h *HotkeyHost) Unregister(action platform.HotkeyAction) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.registered, action)
	return nil
}

// UnregisterAll implements [platform.HotkeyHost].
func (h *HotkeyHost) UnregisterAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.registered)
	return nil
}

// Events implements [platform.HotkeyHost].
func (h *HotkeyHost) Events() (<-chan platform.HotkeyEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events.subscribe(context.Background(), &h.mu)
}

// Notes implements [platform.HotkeyHost]; the fake never has any.
func (h *HotkeyHost) Notes() []string { return nil }
