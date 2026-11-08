package win

import "github.com/BorrowedMirror/rust-king-ledger/internal/platform"

// Low-level hook message identifiers (WinUser.h).
const (
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmMouseMove  = 0x0200
	wmMouseWheel = 0x020A
	// WM_MOUSEHWHEEL (horizontal) and the button messages are not forwarded.

	wheelDelta = 120
	vkPacket   = 0xE7
)

// keyboardEvent translates a WH_KEYBOARD_LL message. vk and scan are KBDLLHOOKSTRUCT's vkCode
// and scanCode. VK_PACKET (injected Unicode) reports the scan code field instead, which then
// holds the character.
func keyboardEvent(message uintptr, vk, scan uint32) (platform.InputEvent, bool) {
	code := vk
	if vk == vkPacket {
		code = scan
	}
	switch message {
	case wmKeyDown, wmSysKeyDown:
		return platform.InputEvent{Type: platform.KeyDown, Key: keyCode(code)}, true
	case wmKeyUp, wmSysKeyUp:
		return platform.InputEvent{Type: platform.KeyUp, Key: keyCode(code)}, true
	}
	return platform.InputEvent{}, false
}

// mouseEvent translates a WH_MOUSE_LL message. x, y are MSLLHOOKSTRUCT.pt and mouseData its
// mouseData (the wheel delta is the signed high word). Buttons and horizontal wheel are
// dropped; wheel deltas are in notches (WHEEL_DELTA units, truncated toward zero), and a zero
// notch count is dropped.
func mouseEvent(message uintptr, x, y int32, mouseData uint32) (platform.InputEvent, bool) {
	switch message {
	case wmMouseMove:
		return platform.InputEvent{Type: platform.MouseMove, X: x, Y: y}, true
	case wmMouseWheel:
		if d := int32(int16(mouseData>>16)) / wheelDelta; d != 0 {
			return platform.InputEvent{Type: platform.Wheel, Delta: d}, true
		}
	}
	return platform.InputEvent{}, false
}
