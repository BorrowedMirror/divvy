package win

import (
	"fmt"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

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

// Option configures [New].
type Option func(*options)

type options struct {
	hookMouse bool
}

// WithMouseHook also installs the low-level mouse hook, for Ctrl + wheel stash tab switching.
// It is off by default: a WH_MOUSE_LL hook adds latency to every mouse move in the game. The
// shell passes the stash.ctrl_scroll_tabs setting read at startup, as the Rust app did.
func WithMouseHook(enabled bool) Option {
	return func(o *options) { o.hookMouse = enabled }
}

// forward is the non-blocking hand-off from a hook callback; it drops the event when the
// consumer lags.
func forward(tx chan<- platform.InputEvent, ev platform.InputEvent) bool {
	select {
	case tx <- ev:
		return true
	default:
		return false
	}
}

// keyboardEvent translates a WH_KEYBOARD_LL message. vk and scan are KBDLLHOOKSTRUCT's vkCode
// and scanCode. As in rdev, VK_PACKET (injected Unicode) uses the scan code field, which then
// holds the character.
func keyboardEvent(msg uintptr, vk, scan uint32) (platform.InputEvent, bool) {
	code := vk
	if vk == vkPacket {
		code = scan
	}
	switch msg {
	case wmKeyDown, wmSysKeyDown:
		return platform.InputEvent{Type: platform.KeyDown, Key: keyCode(code)}, true
	case wmKeyUp, wmSysKeyUp:
		return platform.InputEvent{Type: platform.KeyUp, Key: keyCode(code)}, true
	}
	return platform.InputEvent{}, false
}

// mouseEvent translates a WH_MOUSE_LL message. x, y are MSLLHOOKSTRUCT.pt and mouseData its
// mouseData (the wheel delta is the signed high word). Buttons and horizontal wheel are
// dropped; wheel deltas are in notches (WHEEL_DELTA units, truncated, as rdev does), and a zero
// notch count is dropped.
func mouseEvent(msg uintptr, x, y int32, mouseData uint32) (platform.InputEvent, bool) {
	switch msg {
	case wmMouseMove:
		return platform.InputEvent{Type: platform.MouseMove, X: x, Y: y}, true
	case wmMouseWheel:
		if d := int32(int16(mouseData>>16)) / wheelDelta; d != 0 {
			return platform.InputEvent{Type: platform.Wheel, Delta: d}, true
		}
	}
	return platform.InputEvent{}, false
}

// keyCode maps a virtual-key code to a W3C KeyboardEvent.code name (the format
// [platform.Chord] uses). It reproduces what the Rust backend produced through rdev: rdev's
// Windows virtual-key table (first match wins) followed by its W3C name table, with rdev's
// Debug name for keys W3C has no name for and "Unidentified" for unknown codes.
func keyCode(vk uint32) string {
	switch {
	case vk >= 'A' && vk <= 'Z':
		return "Key" + string(rune(vk))
	case vk >= '0' && vk <= '9':
		return "Digit" + string(rune(vk))
	case vk >= 0x60 && vk <= 0x69:
		return fmt.Sprintf("Numpad%d", vk-0x60)
	case vk >= 0x70 && vk <= 0x87:
		return fmt.Sprintf("F%d", vk-0x70+1)
	}
	if name, ok := vkNames[vk]; ok {
		return name
	}
	return "Unidentified"
}

var vkNames = map[uint32]string{
	0x00: "KanaMode", // rdev's table lists KanaMode with virtual key 0
	0x03: "Cancel",
	0x08: "Backspace",
	0x09: "Tab",
	0x0C: "Clear",
	0x0D: "Enter", // also the numpad Enter: rdev decides by virtual key alone
	0x13: "Pause",
	0x14: "CapsLock",
	0x15: "Kana",
	0x17: "Junja",
	0x18: "Final",
	0x19: "Hanja",
	0x1B: "Escape",
	0x1C: "Convert",
	0x1D: "NonConvert",
	0x20: "Space",
	0x21: "PageUp",
	0x22: "PageDown",
	0x23: "End",
	0x24: "Home",
	0x25: "ArrowLeft",
	0x26: "ArrowUp",
	0x27: "ArrowRight",
	0x28: "ArrowDown",
	0x29: "Select",
	0x2A: "Print",
	0x2B: "Execute",
	0x2C: "PrintScreen",
	0x2D: "Insert",
	0x2E: "Delete",
	0x2F: "Help",
	0x5B: "MetaLeft",
	0x5C: "MetaRight",
	0x5D: "ContextMenu",
	0x5F: "Sleep",
	0x6A: "NumpadMultiply",
	0x6B: "NumpadAdd",
	0x6C: "Separator",
	0x6D: "NumpadSubtract",
	0x6E: "NumpadDecimal",
	0x6F: "NumpadDivide",
	0x90: "NumLock",
	0x91: "ScrollLock",
	0xA0: "ShiftLeft",
	0xA1: "ShiftRight",
	0xA2: "ControlLeft",
	0xA3: "ControlRight",
	0xA4: "AltLeft",
	0xA5: "AltRight",
	0xAD: "AudioVolumeMute",
	0xAE: "AudioVolumeDown",
	0xAF: "AudioVolumeUp",
	0xBA: "Semicolon",
	0xBB: "Equal",
	0xBC: "Comma",
	0xBD: "Minus",
	0xBE: "Period",
	0xBF: "Slash",
	0xC0: "Backquote",
	0xDB: "BracketLeft",
	0xDC: "Backslash",
	0xDD: "BracketRight",
	0xDE: "Quote",
	0xE2: "IntlBackslash",
}
