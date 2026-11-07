package win

import (
	"math"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func info(id uint64, rect platform.Rect, focused bool) platform.WindowInfo {
	return platform.WindowInfo{Title: platform.GameWindowTitle, Rect: rect, Focused: focused, NativeID: id}
}

var (
	r1 = platform.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}
	r2 = platform.Rect{X: 10, Y: 20, Width: 800, Height: 600}
)

func TestTitleMatchingIsExactAfterTrim(t *testing.T) {
	assert.True(t, titleMatches("Path of Exile 2"))
	assert.True(t, titleMatches("  Path of Exile 2\x00"))
	assert.False(t, titleMatches("Path of Exile"))
	assert.False(t, titleMatches("Path of Exile 2 - Wiki - Mozilla Firefox"))
	assert.False(t, titleMatches("path of exile 2"))
	assert.False(t, titleMatches(""))
}

func TestRectConversion(t *testing.T) {
	assert.Equal(t, r2, rectFromLTRB(10, 20, 810, 620))
	assert.Equal(t, platform.Rect{X: -8, Y: -8, Width: 1936, Height: 1096}, rectFromLTRB(-8, -8, 1928, 1088))
	assert.Equal(t, platform.Rect{X: 5, Y: 5, Width: 0, Height: 0}, rectFromLTRB(5, 5, 0, 0))
	assert.Equal(t, uint32(math.MaxUint32), rectFromLTRB(math.MinInt32, 0, math.MaxInt32, 1).Width)
}

func TestExStyles(t *testing.T) {
	const base uint32 = 0x100
	p := preparedExStyle(base)
	assert.Equal(t, base|wsExNoActivate|wsExToolWindow|wsExTopmost, p)
	assert.Equal(t, p, preparedExStyle(p))

	on, added := clickThroughExStyle(p, true, false)
	assert.Equal(t, p|wsExLayered|wsExTransparent, on)
	assert.True(t, added)
	on2, added2 := clickThroughExStyle(on, true, added)
	assert.Equal(t, on, on2)
	assert.True(t, added2)
	off, added := clickThroughExStyle(on, false, added)
	assert.Equal(t, p, off)
	assert.False(t, added)

	// Already layered by its owner: keep WS_EX_LAYERED when turning click-through off.
	layered := p | wsExLayered
	on, added = clickThroughExStyle(layered, true, false)
	assert.False(t, added)
	off, _ = clickThroughExStyle(on, false, added)
	assert.Equal(t, layered, off)
}

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

func TestTrackerFoundMovedFocusLost(t *testing.T) {
	var s trackerState
	_, ok := s.gameID()
	assert.False(t, ok)
	assert.Empty(t, s.onScan(nil))
	_, ok = s.onForeground(1)
	assert.False(t, ok)
	_, ok = s.onMoved(1, r2)
	assert.False(t, ok)

	found := info(1, r1, false)
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowFound, Info: info(1, r1, false)}}, s.onScan(&found))
	id, ok := s.gameID()
	assert.True(t, ok)
	assert.Equal(t, uint64(1), id)
	same := info(1, r1, false)
	assert.Empty(t, s.onScan(&same))

	ev, ok := s.onForeground(1)
	assert.True(t, ok)
	assert.Equal(t, platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: true}, ev)
	_, ok = s.onForeground(1)
	assert.False(t, ok)
	ev, ok = s.onForeground(2)
	assert.True(t, ok)
	assert.Equal(t, platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: false}, ev)

	_, ok = s.onMoved(2, r2)
	assert.False(t, ok, "other window")
	ev, ok = s.onMoved(1, r2)
	assert.True(t, ok)
	assert.Equal(t, platform.WindowEvent{Type: platform.WindowMoved, Rect: r2}, ev)
	_, ok = s.onMoved(1, r2)
	assert.False(t, ok)

	back := info(1, r1, true)
	assert.Equal(t, []platform.WindowEvent{
		{Type: platform.WindowMoved, Rect: r1},
		{Type: platform.WindowFocusChanged, Focused: true},
	}, s.onScan(&back))

	// Game restarted: new HWND.
	restarted := info(3, r1, true)
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowFound, Info: info(3, r1, true)}}, s.onScan(&restarted))
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowLost}}, s.onScan(nil))
	_, ok = s.gameID()
	assert.False(t, ok)
}

func TestTrackerKeepsItsOwnCopy(t *testing.T) {
	var s trackerState
	found := info(1, r1, false)
	s.onScan(&found)
	found.Rect = r2 // the caller's value must not alias the tracked state
	_, ok := s.onMoved(1, r1)
	assert.False(t, ok)
}

func TestCapabilityReport(t *testing.T) {
	c := capabilities()
	assert.True(t, c.WindowTracking && c.OverlayAlwaysOnTop && c.OverlayClickThrough)
	assert.True(t, c.PassiveHook && c.KeystrokeInjection)
	// Hotkeys and the clipboard come from Wails; the shell sets those two.
	assert.False(t, c.GlobalHotkeys || c.Clipboard)
	assert.Empty(t, c.MissingPermissions)
	assert.Len(t, c.Notes, 2)
}

func TestOverlayRequiresWin32Handle(t *testing.T) {
	_, err := hwndFrom(nil)
	assert.ErrorIs(t, err, platform.ErrUnsupported)
	var fake byte
	hwnd, err := hwndFrom(unsafe.Pointer(&fake))
	require.NoError(t, err)
	assert.Equal(t, uintptr(unsafe.Pointer(&fake)), hwnd)
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

func TestMapsEvents(t *testing.T) {
	check := func(ev platform.InputEvent, ok bool) func(platform.InputEvent, bool) {
		return func(want platform.InputEvent, wantOK bool) {
			t.Helper()
			assert.Equal(t, wantOK, ok)
			assert.Equal(t, want, ev)
		}
	}
	check(keyboardEvent(wmKeyDown, 'A', 0x1E))(platform.InputEvent{Type: platform.KeyDown, Key: "KeyA"}, true)
	check(keyboardEvent(wmKeyUp, 'A', 0x1E))(platform.InputEvent{Type: platform.KeyUp, Key: "KeyA"}, true)
	// Alt held turns key messages into WM_SYS*.
	check(keyboardEvent(wmSysKeyDown, 0xA4, 0x38))(platform.InputEvent{Type: platform.KeyDown, Key: "AltLeft"}, true)
	check(keyboardEvent(wmSysKeyUp, 'D', 0x20))(platform.InputEvent{Type: platform.KeyUp, Key: "KeyD"}, true)
	// VK_PACKET reads the scan code field.
	check(keyboardEvent(wmKeyDown, vkPacket, 'C'))(platform.InputEvent{Type: platform.KeyDown, Key: "KeyC"}, true)

	check(mouseEvent(wmMouseMove, 10, -2, 0))(platform.InputEvent{Type: platform.MouseMove, X: 10, Y: -2}, true)
	wheel := func(delta int16) uint32 { return uint32(uint16(delta)) << 16 }
	check(mouseEvent(wmMouseWheel, 0, 0, wheel(-120)))(platform.InputEvent{Type: platform.Wheel, Delta: -1}, true)
	check(mouseEvent(wmMouseWheel, 0, 0, wheel(240)))(platform.InputEvent{Type: platform.Wheel, Delta: 2}, true)
	check(mouseEvent(wmMouseWheel, 0, 0, wheel(60)))(platform.InputEvent{}, false) // under one notch
	check(mouseEvent(0x020E, 0, 0, wheel(120)))(platform.InputEvent{}, false)      // WM_MOUSEHWHEEL
	check(mouseEvent(0x0201, 0, 0, 0))(platform.InputEvent{}, false)               // WM_LBUTTONDOWN
}

func TestForwardNeverBlocks(t *testing.T) {
	tx := make(chan platform.InputEvent, 1)
	assert.True(t, forward(tx, platform.InputEvent{Type: platform.Wheel, Delta: 1}))
	assert.False(t, forward(tx, platform.InputEvent{Type: platform.Wheel, Delta: 2}), "full channel drops")
	// Go channels have no receiver-gone state; an unread channel just stays full.
	assert.False(t, forward(tx, platform.InputEvent{Type: platform.Wheel, Delta: 3}), "unread channel drops")
}

func TestQueueKeepsOrderAndNeverBlocks(t *testing.T) {
	q := newQueue[int]()
	for i := range 1000 { // far more than any channel buffer: push must not block
		q.push(i)
	}
	for i := range 1000 {
		select {
		case got := <-q.out:
			require.Equal(t, i, got)
		case <-time.After(5 * time.Second):
			t.Fatal("queue stalled")
		}
	}
}

func TestOptions(t *testing.T) {
	var o options
	WithMouseHook(true)(&o)
	assert.True(t, o.hookMouse)
}
