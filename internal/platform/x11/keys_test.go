package x11

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestMapsKeysToW3CCodes(t *testing.T) {
	for kc, want := range map[uint8]string{
		54:  "KeyC",
		16:  "Digit7",
		71:  "F5",
		9:   "Escape",
		37:  "ControlLeft",
		36:  "Enter",
		90:  "Numpad0",
		104: "NumpadEnter",
		198: "F20",
		127: "Pause",
		255: "Unidentified",
		130: "Hangul",
		131: "Hanja",
		64:  "AltLeft",
		108: "AltRight",
	} {
		assert.Equal(t, want, keyCode(kc), "keycode %d", kc)
	}
}

// Named keys map back to the same keycode, and no name is used twice.
func TestNamedKeysRoundTrip(t *testing.T) {
	for _, code := range []string{"Escape", "KeyA", "Digit0", "F1", "Period", "Backquote", "Insert", "Numpad9", "IntlBackslash"} {
		kc, ok := keycodeFor(code)
		assert.True(t, ok, code)
		assert.Equal(t, code, keyCode(kc))
	}
	assert.Len(t, keycodesByName, len(keycodeNames), "names are unique")
}

func TestMapsChordKeys(t *testing.T) {
	for code, want := range map[string]uint32{
		"KeyC": 'c', "Digit4": '4', "F12": 0xffc9, "F1": 0xffbe, "F10": 0xffc7, "Escape": xkEscape,
		"PageDown": xkNext, "ArrowLeft": xkLeft, "ArrowRight": xkRight,
	} {
		got, ok := chordKeysym(code)
		assert.True(t, ok, code)
		assert.Equal(t, want, got, code)
	}
	for _, code := range []string{"Key1", "DigitX", "Pause", "F13", "F01", "", "KeyAB"} {
		_, ok := chordKeysym(code)
		assert.False(t, ok, code)
	}
	assert.Equal(t, uint32(xkSuperL), modifierKeysym(platform.Meta))
	assert.Equal(t, uint32(xkControlL), modifierKeysym(platform.Ctrl))
}

func TestKeysymToKeycodePrefersFirstColumn(t *testing.T) {
	// keycodes 8..10, two columns each: (a A) (b A) (c C)
	syms := []uint32{'a', 'A', 'b', 'A', 'A', 'C'}
	kc, ok := keysymToKeycode(8, 2, syms, 'A')
	assert.True(t, ok)
	assert.Equal(t, uint8(10), kc, "column 0 first")
	kc, ok = keysymToKeycode(8, 2, syms, 'b')
	assert.True(t, ok)
	assert.Equal(t, uint8(9), kc)
	_, ok = keysymToKeycode(8, 2, syms, 'z')
	assert.False(t, ok)
	_, ok = keysymToKeycode(8, 0, syms, 'a')
	assert.False(t, ok, "no columns")
}
