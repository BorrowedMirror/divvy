package win

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestVirtualKeys(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected uint16
	}{
		{
			"KeyC",
			"KeyC",
			0x43,
		},
		{
			"KeyA",
			"KeyA",
			0x41,
		},
		{
			"Digit0",
			"Digit0",
			0x30,
		},
		{
			"Digit9",
			"Digit9",
			0x39,
		},
		{
			"Numpad7",
			"Numpad7",
			0x67,
		},
		{
			"F1",
			"F1",
			0x70,
		},
		{
			"F12",
			"F12",
			0x7B,
		},
		{
			"F24",
			"F24",
			0x87,
		},
		{
			"Space",
			"Space",
			0x20,
		},
		{
			"Quote",
			"Quote",
			0xDE,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := vkForCode(tc.have)
			assert.True(t, ok)
			assert.Equal(t, tc.expected, got)
		})
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
	testCases := []struct {
		name     string
		have     uint32
		expected string
	}{
		{
			"KeyC",
			'C',
			"KeyC",
		},
		{
			"Digit7",
			'7',
			"Digit7",
		},
		{
			"F5",
			0x74,
			"F5",
		},
		{
			"Escape",
			0x1B,
			"Escape",
		},
		{
			"ControlLeft",
			0xA2,
			"ControlLeft",
		},
		{
			"Enter",
			0x0D,
			"Enter",
		},
		{
			"Numpad0",
			0x60,
			"Numpad0",
		},
		{
			"F20",
			0x83,
			"F20",
		},
		{
			"Pause",
			0x13,
			"Pause",
		},
		{
			"Kana",
			0x15,
			"Kana",
		},
		{
			"Help",
			0x2F,
			"Help",
		},
		{
			"AltLeft",
			0xA4,
			"AltLeft",
		},
		{
			"AltRight",
			0xA5,
			"AltRight",
		},
		{
			"MetaLeft",
			0x5B,
			"MetaLeft",
		},
		{
			"Period",
			0xBE,
			"Period",
		},
		{
			"GenericShiftIsUnidentified",
			0x10, // generic VK_SHIFT: low-level hooks report left/right instead
			"Unidentified",
		},
		{
			"UnknownIsUnidentified",
			999,
			"Unidentified",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, keyCode(tc.have))
		})
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
