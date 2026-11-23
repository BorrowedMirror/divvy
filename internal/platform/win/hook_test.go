package win

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

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
