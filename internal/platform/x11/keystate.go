package x11

// Physical key state on X11 (QueryKeymap + GetModifierMapping).
//
// Used before synthesising the copy chord:
//   - A global hotkey triggers on an XGrabKey passive grab, which turns into an active keyboard
//     grab for as long as the hotkey's key is held. While that grab is active every key event,
//     including our XTEST Ctrl+C, is delivered to the grabbing client (us), not the game. So we
//     first wait (capped) until no non-modifier key is down.
//   - Modifiers still held (Ctrl from Ctrl+D) feed [platform.PlanChord].

import (
	"slices"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Keymap is a QueryKeymap bit vector: bit k set = keycode k is down.
type Keymap [32]byte

// releaseWait is how long the simulator waits for the hotkey key to come up before sending
// anyway.
const releaseWait = 400 * time.Millisecond

const releasePoll = 5 * time.Millisecond

func isDown(keys *Keymap, keycode uint8) bool {
	return keys[keycode/8]&(1<<(keycode%8)) != 0
}

// modifierMap is the server's modifier mapping: eight rows (Shift, Lock, Control, Mod1..Mod5)
// of keycodes.
type modifierMap struct {
	rows [8][]uint8
}

// modifierRow is the modifier mapping row for each modifier we model (Mod1 = Alt, Mod4 = Super:
// the near-universal XKB defaults).
func modifierRow(m platform.KeyModifier) int {
	switch m {
	case platform.Shift:
		return 0
	case platform.Ctrl:
		return 2
	case platform.Alt:
		return 3
	default: // Meta
		return 6
	}
}

var allModifiers = [...]platform.KeyModifier{platform.Ctrl, platform.Alt, platform.Shift, platform.Meta}

// modifierMapFromReply builds the map from a GetModifierMapping reply
// (len(keycodes) == 8 * keycodes_per_modifier; 0 = unused).
func modifierMapFromReply(keycodes []uint8) modifierMap {
	var m modifierMap
	per := len(keycodes) / 8
	if per == 0 {
		return m
	}
	for i := range m.rows {
		row := []uint8{}
		for _, k := range keycodes[i*per : (i+1)*per] {
			if k != 0 {
				row = append(row, k)
			}
		}
		m.rows[i] = row
	}
	return m
}

// isModifier reports whether keycode is bound to any modifier (incl. Lock, NumLock,
// ISO_Level3...).
func (m *modifierMap) isModifier(keycode uint8) bool {
	for _, r := range m.rows {
		if slices.Contains(r, keycode) {
			return true
		}
	}
	return false
}

// heldKeycodes are the keycodes of mod that are down.
func (m *modifierMap) heldKeycodes(mod platform.KeyModifier, keys *Keymap) []uint8 {
	var out []uint8
	for _, k := range m.rows[modifierRow(mod)] {
		if isDown(keys, k) {
			out = append(out, k)
		}
	}
	return out
}

// held are the modifiers with at least one key down.
func (m *modifierMap) held(keys *Keymap) []platform.KeyModifier {
	var out []platform.KeyModifier
	for _, mod := range allModifiers {
		if len(m.heldKeycodes(mod, keys)) > 0 {
			out = append(out, mod)
		}
	}
	return out
}

// nonModifierDown reports whether any key that is not a modifier is down (e.g. the D of
// Ctrl+D).
func (m *modifierMap) nonModifierDown(keys *Keymap) bool {
	for k := 8; k <= 255; k++ {
		if isDown(keys, uint8(k)) && !m.isModifier(uint8(k)) {
			return true
		}
	}
	return false
}

// waitForRelease polls query until no non-modifier key is down or limit elapsed, and returns
// the last keymap; ok is false when the query failed. sleep is injected for tests.
func waitForRelease(m *modifierMap, limit time.Duration, query func() (Keymap, bool), sleep func(time.Duration)) (Keymap, bool) {
	start := time.Now()
	var waited time.Duration
	for {
		keys, ok := query()
		if !ok {
			return Keymap{}, false
		}
		if !m.nonModifierDown(&keys) || waited >= limit || time.Since(start) >= limit {
			return keys, true
		}
		sleep(releasePoll)
		waited += releasePoll
	}
}

// heldState is what the simulator needs right before sending a chord.
type heldState struct {
	modifiers []platform.KeyModifier
	// keycodes are the held keycodes per modifier in modifiers (so the exact key, e.g.
	// Shift_R, is released).
	keycodes map[platform.KeyModifier][]uint8
}

func heldStateFromKeymap(m *modifierMap, keys *Keymap) heldState {
	h := heldState{modifiers: m.held(keys), keycodes: map[platform.KeyModifier][]uint8{}}
	for _, mod := range h.modifiers {
		h.keycodes[mod] = m.heldKeycodes(mod, keys)
	}
	return h
}

func (h *heldState) keycodesOf(m platform.KeyModifier) []uint8 {
	return h.keycodes[m]
}
