package win

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestVirtualKeys(t *testing.T) {
	for code, want := range map[string]uint16{
		"KeyC": 0x43, "KeyA": 0x41, "Digit0": 0x30, "Digit9": 0x39, "Numpad7": 0x67,
		"F1": 0x70, "F12": 0x7B, "F24": 0x87, "Space": 0x20, "Quote": 0xDE,
	} {
		got, ok := vkForCode(code)
		assert.True(t, ok, code)
		assert.Equal(t, want, got, code)
	}
	for _, bad := range []string{"Keyc", "KeyAB", "Key", "Digit", "Digit10", "F0", "F25", "F01", "Numpad10", "Nope", ""} {
		_, ok := vkForCode(bad)
		assert.False(t, ok, bad)
	}
	assert.Equal(t, vkControl, modifierVK(platform.Ctrl))
	assert.Equal(t, vkMenu, modifierVK(platform.Alt))
	assert.Equal(t, vkShift, modifierVK(platform.Shift))
	assert.Equal(t, vkLWin, modifierVK(platform.Meta))
}

func TestExtendedKeys(t *testing.T) {
	for _, code := range []string{"ArrowUp", "Insert", "Delete", "Home", "End", "PageUp", "PageDown", "PrintScreen", "NumpadDivide"} {
		vk, ok := vkForCode(code)
		require.True(t, ok, code)
		assert.True(t, isExtendedVK(vk), code)
	}
	for _, code := range []string{"KeyC", "Digit1", "F5", "Enter", "Escape", "Numpad7"} {
		vk, _ := vkForCode(code)
		assert.False(t, isExtendedVK(vk), code)
	}
	assert.False(t, isExtendedVK(vkControl))
}

func TestMapsKeysToW3CCodes(t *testing.T) {
	for vk, want := range map[uint32]string{
		'C':  "KeyC",
		'7':  "Digit7",
		0x74: "F5",
		0x1B: "Escape",
		0xA2: "ControlLeft",
		0x0D: "Enter",
		0x60: "Numpad0",
		0x83: "F20",
		0x13: "Pause",
		0x15: "Kana",
		0x2F: "Help",
		0xA4: "AltLeft",
		0xA5: "AltRight",
		0x5B: "MetaLeft",
		0xBE: "Period",
		0x10: "Unidentified", // generic VK_SHIFT: low-level hooks report left/right instead
		999:  "Unidentified",
	} {
		assert.Equal(t, want, keyCode(vk), "vk %#x", vk)
	}
}

// Named keys map back to the same virtual key through the injection table.
func TestNamedKeysRoundTrip(t *testing.T) {
	for _, vk := range []uint16{0x1B, 'A', '0', 0x70, 0xBE, 0xC0, 0x2D, 0x69, 0xDE, 0x20, 0x0D} {
		code := keyCode(uint32(vk))
		back, ok := vkForCode(code)
		assert.True(t, ok, code)
		assert.Equal(t, vk, back, code)
	}
}
