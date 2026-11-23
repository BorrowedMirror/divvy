package mac

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func keycode(t *testing.T, code string) uint16 {
	t.Helper()
	k, ok := macKeycode(code)
	require.True(t, ok, code)
	return k
}

func TestMapsKnownKeys(t *testing.T) {
	assert.Equal(t, uint16(0x08), keycode(t, "KeyC"))
	assert.Equal(t, uint16(0x02), keycode(t, "KeyD"))
	assert.Equal(t, uint16(0x60), keycode(t, "F5"))
	assert.Equal(t, uint16(0x35), keycode(t, "Escape"))
	_, ok := macKeycode("Nope")
	assert.False(t, ok)
	code, ok := codeForKeycode(0x08)
	assert.True(t, ok)
	assert.Equal(t, "KeyC", code)
	_, ok = codeForKeycode(0x42)
	assert.False(t, ok)
	code, _ = codeForKeycode(0x35)
	assert.Equal(t, "Escape", code)
}

func TestTableIsABijection(t *testing.T) {
	for _, e := range keyTable {
		assert.Equal(t, e.keycode, keycode(t, e.code), e.code)
		code, ok := codeForKeycode(e.keycode)
		assert.True(t, ok)
		assert.Equal(t, e.code, code, "%#x", e.keycode)
	}
}

func TestCoversEveryLetterDigitAndFunctionKey(t *testing.T) {
	for c := 'A'; c <= 'Z'; c++ {
		keycode(t, fmt.Sprintf("Key%c", c))
	}
	for d := range 10 {
		keycode(t, fmt.Sprintf("Digit%d", d))
		keycode(t, fmt.Sprintf("Numpad%d", d))
	}
	for f := 1; f <= 20; f++ {
		keycode(t, fmt.Sprintf("F%d", f))
	}
}

func TestChordParseOutputIsMappable(t *testing.T) {
	// ParseChord emits KeyX / DigitN; those must all resolve.
	chord, err := platform.ParseChord("ctrl+alt+c")
	require.NoError(t, err)
	assert.Equal(t, uint16(0x08), keycode(t, chord.Key))
}

func TestModifierKeysAreInTheKeymap(t *testing.T) {
	for m, key := range modifierKeys {
		code, ok := codeForKeycode(key.keycode)
		assert.True(t, ok, m)
		assert.Contains(t, code, "Left", m)
		assert.Equal(t, []platform.KeyModifier{m}, heldFromFlags(key.flags), m)
	}
	assert.Equal(t, injectBaseFlags, injectFlags(nil))
	assert.Equal(t, []platform.KeyModifier{platform.Ctrl, platform.Meta},
		heldFromFlags(injectFlags([]platform.KeyModifier{platform.Meta, platform.Ctrl})))
}

func TestHeldModifiersFromCGFlags(t *testing.T) {
	assert.Empty(t, heldFromFlags(0))
	assert.Equal(t, []platform.KeyModifier{platform.Ctrl, platform.Shift}, heldFromFlags(flagControl|flagShift|0x100))
	assert.Equal(t, []platform.KeyModifier{platform.Alt, platform.Meta}, heldFromFlags(flagAlternate|flagCommand))
}
