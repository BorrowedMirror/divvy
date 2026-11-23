package x11

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestMapsKeysToW3CCodes(t *testing.T) {
	testCases := []struct {
		name     string
		have     uint8
		expected string
	}{
		{
			"KeyC",
			54,
			"KeyC",
		},
		{
			"Digit7",
			16,
			"Digit7",
		},
		{
			"F5",
			71,
			"F5",
		},
		{
			"Escape",
			9,
			"Escape",
		},
		{
			"ControlLeft",
			37,
			"ControlLeft",
		},
		{
			"Enter",
			36,
			"Enter",
		},
		{
			"Numpad0",
			90,
			"Numpad0",
		},
		{
			"NumpadEnter",
			104,
			"NumpadEnter",
		},
		{
			"F20",
			198,
			"F20",
		},
		{
			"Pause",
			127,
			"Pause",
		},
		{
			"UnknownIsUnidentified",
			255,
			"Unidentified",
		},
		{
			"Hangul",
			130,
			"Hangul",
		},
		{
			"Hanja",
			131,
			"Hanja",
		},
		{
			"AltLeft",
			64,
			"AltLeft",
		},
		{
			"AltRight",
			108,
			"AltRight",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, keyCode(tc.have))
		})
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
	testCases := []struct {
		name     string
		have     string
		expected uint32
	}{
		{
			"KeyC",
			"KeyC",
			'c',
		},
		{
			"Digit4",
			"Digit4",
			'4',
		},
		{
			"F12",
			"F12",
			0xffc9,
		},
		{
			"F1",
			"F1",
			0xffbe,
		},
		{
			"F10",
			"F10",
			0xffc7,
		},
		{
			"Escape",
			"Escape",
			xkEscape,
		},
		{
			"PageDown",
			"PageDown",
			xkNext,
		},
		{
			"ArrowLeft",
			"ArrowLeft",
			xkLeft,
		},
		{
			"ArrowRight",
			"ArrowRight",
			xkRight,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := chordKeysym(tc.have)
			assert.True(t, ok)
			assert.Equal(t, tc.expected, got)
		})
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
