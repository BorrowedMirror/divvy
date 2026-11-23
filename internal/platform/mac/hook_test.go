package mac

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestMapsKeycodesToW3CCodes(t *testing.T) {
	testCases := []struct {
		name     string
		have     uint16
		expected string
	}{
		{
			"KeyC",
			0x08,
			"KeyC",
		},
		{
			"Digit7",
			0x1A,
			"Digit7",
		},
		{
			"F5",
			0x60,
			"F5",
		},
		{
			"Escape",
			0x35,
			"Escape",
		},
		{
			"ControlLeft",
			0x3B,
			"ControlLeft",
		},
		{
			"Enter",
			0x24,
			"Enter",
		},
		{
			"Numpad0",
			0x52,
			"Numpad0",
		},
		{
			"NumpadEnter",
			0x4C,
			"NumpadEnter",
		},
		{
			"F20",
			0x5A,
			"F20",
		},
		{
			"Fn",
			0x3F,
			"Fn",
		},
		{
			"Lang1",
			0x68,
			"Lang1",
		},
		{
			"ContextMenu",
			0x6E,
			"ContextMenu",
		},
		{
			"UnmappedIsUnidentified",
			0x42,
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
			assert.Equal(t, tc.expected, keyName(tc.have))
		})
	}
}

// Every injectable key maps back to the same name, and the hook-only keys never collide with it.
func TestHookNamesRoundTripThroughKeymap(t *testing.T) {
	for _, e := range keyTable {
		assert.Equal(t, e.code, keyName(e.keycode))
	}
	for k, name := range hookOnlyKeys {
		_, inTable := codeForKeycode(k)
		assert.False(t, inTable, "%#x", k)
		_, injectable := macKeycode(name)
		assert.False(t, injectable, name)
	}
}

func TestMapsEvents(t *testing.T) {
	var st hookState
	ev := func(e cgEvent) *platform.InputEvent {
		if got, ok := st.mapEvent(e); ok {
			return &got
		}
		return nil
	}
	assert.Equal(t, &platform.InputEvent{Type: platform.KeyDown, Key: "KeyA"}, ev(cgEvent{Type: cgEventKeyDown, Keycode: 0}))
	assert.Equal(t, &platform.InputEvent{Type: platform.KeyUp, Key: "KeyA"}, ev(cgEvent{Type: cgEventKeyUp, Keycode: 0}))
	assert.Equal(t, &platform.InputEvent{Type: platform.MouseMove, X: 10, Y: -2},
		ev(cgEvent{Type: cgEventMouseMoved, X: 10.6, Y: -2.0}))
	assert.Equal(t, &platform.InputEvent{Type: platform.Wheel, Delta: -1}, ev(cgEvent{Type: cgEventScrollWheel, WheelY: -1}))
	assert.Nil(t, ev(cgEvent{Type: cgEventScrollWheel, WheelY: 0}))
	assert.Nil(t, ev(cgEvent{Type: 1})) // left mouse down
	assert.Nil(t, ev(cgEvent{Type: cgEventTapDisabledTimeout}))
	assert.Nil(t, ev(cgEvent{Type: cgEventKeyDown, Keycode: -1}))

	// Modifier changes: more flags is a press, fewer a release.
	assert.Equal(t, &platform.InputEvent{Type: platform.KeyDown, Key: "MetaLeft"},
		ev(cgEvent{Type: cgEventFlagsChanged, Keycode: 0x37, Flags: flagCommand | 0x100}))
	assert.Equal(t, &platform.InputEvent{Type: platform.KeyDown, Key: "ShiftLeft"},
		ev(cgEvent{Type: cgEventFlagsChanged, Keycode: 0x38, Flags: flagCommand | flagShift | 0x100}))
	assert.Equal(t, &platform.InputEvent{Type: platform.KeyUp, Key: "MetaLeft"},
		ev(cgEvent{Type: cgEventFlagsChanged, Keycode: 0x37, Flags: flagShift | 0x100}))
}

func TestEventMaskIsKeyboardOnlyUnlessWheel(t *testing.T) {
	keys := hookEventMask(false)
	assert.Equal(t, uint64(1<<10|1<<11|1<<12), keys)
	withMouse := hookEventMask(true)
	assert.NotZero(t, withMouse&(1<<cgEventScrollWheel))
	assert.Equal(t, keys, withMouse&keys)
}
