package win

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Virtual-key codes for modifiers.
const (
	vkShift   uint16 = 0x10
	vkControl uint16 = 0x11
	vkMenu    uint16 = 0x12
	vkLWin    uint16 = 0x5B
)

// vkForCode maps a W3C KeyboardEvent.code name to a Windows virtual-key code (layout-independent
// for the letter/digit row, which is what games bind to).
func vkForCode(code string) (uint16, bool) {
	if rest, ok := strings.CutPrefix(code, "Key"); ok {
		if len(rest) == 1 && rest[0] >= 'A' && rest[0] <= 'Z' {
			return uint16(rest[0]), true
		}
		return 0, false
	}
	if rest, ok := strings.CutPrefix(code, "Digit"); ok {
		if len(rest) == 1 && rest[0] >= '0' && rest[0] <= '9' {
			return uint16(rest[0]), true
		}
		return 0, false
	}
	if rest, ok := strings.CutPrefix(code, "Numpad"); ok && len(rest) == 1 && rest[0] >= '0' && rest[0] <= '9' {
		return 0x60 + uint16(rest[0]-'0'), true
	}
	if rest, ok := strings.CutPrefix(code, "F"); ok && !strings.HasPrefix(rest, "0") {
		if n, err := strconv.ParseUint(rest, 10, 8); err == nil && n >= 1 && n <= 24 {
			return 0x70 + uint16(n) - 1, true
		}
	}
	vk, ok := namedVK[code]
	return vk, ok
}

var namedVK = map[string]uint16{
	"Backspace":      0x08,
	"Tab":            0x09,
	"Enter":          0x0D,
	"NumpadEnter":    0x0D,
	"Pause":          0x13,
	"CapsLock":       0x14,
	"Escape":         0x1B,
	"Space":          0x20,
	"PageUp":         0x21,
	"PageDown":       0x22,
	"End":            0x23,
	"Home":           0x24,
	"ArrowLeft":      0x25,
	"ArrowUp":        0x26,
	"ArrowRight":     0x27,
	"ArrowDown":      0x28,
	"PrintScreen":    0x2C,
	"Insert":         0x2D,
	"Delete":         0x2E,
	"NumpadMultiply": 0x6A,
	"NumpadAdd":      0x6B,
	"NumpadSubtract": 0x6D,
	"NumpadDecimal":  0x6E,
	"NumpadDivide":   0x6F,
	"ScrollLock":     0x91,
	"Semicolon":      0xBA,
	"Equal":          0xBB,
	"Comma":          0xBC,
	"Minus":          0xBD,
	"Period":         0xBE,
	"Slash":          0xBF,
	"Backquote":      0xC0,
	"BracketLeft":    0xDB,
	"Backslash":      0xDC,
	"BracketRight":   0xDD,
	"Quote":          0xDE,
}

// modifierVK is the virtual-key code used to query or press a modifier.
func modifierVK(m platform.KeyModifier) uint16 {
	switch m {
	case platform.Ctrl:
		return vkControl
	case platform.Alt:
		return vkMenu
	case platform.Shift:
		return vkShift
	default:
		return vkLWin
	}
}

// isExtendedVK reports whether a virtual key needs KEYEVENTF_EXTENDEDKEY when injected: the
// navigation block (so Shift+navigation does not depend on NumLock), right Ctrl/Alt, NumLock,
// PrintScreen and numpad divide.
func isExtendedVK(vk uint16) bool {
	switch vk {
	case 0xA5, 0xA3, // VK_RMENU, VK_RCONTROL
		0x26, 0x28, 0x25, 0x27, // VK_UP, VK_DOWN, VK_LEFT, VK_RIGHT
		0x2D, 0x2E, 0x24, 0x23, 0x21, 0x22, // VK_INSERT, VK_DELETE, VK_HOME, VK_END, VK_PRIOR, VK_NEXT
		0x90, 0x2C, 0x6F: // VK_NUMLOCK, VK_SNAPSHOT, VK_DIVIDE
		return true
	}
	return false
}

// keyCode maps a virtual-key code to a W3C KeyboardEvent.code name (the format
// [platform.Chord] uses). Keys W3C has no code for keep a descriptive name (Kana, Junja,
// Separator...); unknown codes are "Unidentified". The mapping goes by virtual key alone, so
// the generic VK_SHIFT (low-level hooks report the left or right key instead) is unknown.
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
	0x00: "KanaMode", // some layouts report the Kana key without a virtual key
	0x03: "Cancel",
	0x08: "Backspace",
	0x09: "Tab",
	0x0C: "Clear",
	0x0D: "Enter", // also the numpad Enter: the mapping goes by virtual key alone
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
