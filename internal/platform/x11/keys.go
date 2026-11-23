package x11

import (
	"strconv"
	"strings"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// keycodeNames maps X keycodes (evdev code + 8, the layout-independent physical key) to W3C
// KeyboardEvent.code names. Keys W3C has no code for keep a descriptive name (Hangul, Hanja).
var keycodeNames = map[uint8]string{
	9: "Escape", 10: "Digit1", 11: "Digit2", 12: "Digit3", 13: "Digit4", 14: "Digit5",
	15: "Digit6", 16: "Digit7", 17: "Digit8", 18: "Digit9", 19: "Digit0", 20: "Minus",
	21: "Equal", 22: "Backspace", 23: "Tab",
	24: "KeyQ", 25: "KeyW", 26: "KeyE", 27: "KeyR", 28: "KeyT", 29: "KeyY", 30: "KeyU",
	31: "KeyI", 32: "KeyO", 33: "KeyP", 34: "BracketLeft", 35: "BracketRight", 36: "Enter",
	37: "ControlLeft",
	38: "KeyA", 39: "KeyS", 40: "KeyD", 41: "KeyF", 42: "KeyG", 43: "KeyH", 44: "KeyJ",
	45: "KeyK", 46: "KeyL", 47: "Semicolon", 48: "Quote", 49: "Backquote", 50: "ShiftLeft",
	51: "Backslash",
	52: "KeyZ", 53: "KeyX", 54: "KeyC", 55: "KeyV", 56: "KeyB", 57: "KeyN", 58: "KeyM",
	59: "Comma", 60: "Period", 61: "Slash", 62: "ShiftRight", 63: "NumpadMultiply",
	64: "AltLeft", 65: "Space", 66: "CapsLock",
	67: "F1", 68: "F2", 69: "F3", 70: "F4", 71: "F5", 72: "F6", 73: "F7", 74: "F8", 75: "F9",
	76: "F10", 77: "NumLock", 78: "ScrollLock",
	79: "Numpad7", 80: "Numpad8", 81: "Numpad9", 82: "NumpadSubtract", 83: "Numpad4",
	84: "Numpad5", 85: "Numpad6", 86: "NumpadAdd", 87: "Numpad1", 88: "Numpad2", 89: "Numpad3",
	90: "Numpad0", 91: "NumpadDecimal", 93: "Lang5", 94: "IntlBackslash", 95: "F11", 96: "F12",
	97: "IntlRo", 98: "Lang3", 99: "Lang4", 100: "Convert", 101: "KanaMode", 102: "NonConvert",
	104: "NumpadEnter", 105: "ControlRight", 106: "NumpadDivide", 107: "PrintScreen",
	108: "AltRight", 110: "Home", 111: "ArrowUp", 112: "PageUp", 113: "ArrowLeft",
	114: "ArrowRight", 115: "End", 116: "ArrowDown", 117: "PageDown", 118: "Insert",
	119: "Delete", 121: "AudioVolumeMute", 122: "AudioVolumeDown", 123: "AudioVolumeUp",
	125: "NumpadEqual", 127: "Pause", 129: "NumpadComma", 130: "Hangul", 131: "Hanja",
	132: "IntlYen", 133: "MetaLeft", 134: "MetaRight", 135: "ContextMenu",
	191: "F13", 192: "F14", 193: "F15", 194: "F16", 195: "F17", 196: "F18", 197: "F19",
	198: "F20", 199: "F21", 200: "F22", 201: "F23", 202: "F24",
}

// keyCode is the W3C code name of an X keycode ("Unidentified" for keys not in the table).
func keyCode(keycode uint8) string {
	if name, ok := keycodeNames[keycode]; ok {
		return name
	}
	return "Unidentified"
}

// keycodesByName is the inverse of [keycodeNames].
var keycodesByName = func() map[string]uint8 {
	m := make(map[string]uint8, len(keycodeNames))
	for kc, name := range keycodeNames {
		m[name] = kc
	}
	return m
}()

// keycodeFor is the inverse of [keyCode].
func keycodeFor(code string) (uint8, bool) {
	kc, ok := keycodesByName[code]
	return kc, ok
}

// X keysyms (X11/keysymdef.h).
const (
	xkBackSpace = 0xff08
	xkTab       = 0xff09
	xkReturn    = 0xff0d
	xkEscape    = 0xff1b
	xkHome      = 0xff50
	xkLeft      = 0xff51
	xkUp        = 0xff52
	xkRight     = 0xff53
	xkDown      = 0xff54
	xkPrior     = 0xff55
	xkNext      = 0xff56
	xkEnd       = 0xff57
	xkF1        = 0xffbe
	xkShiftL    = 0xffe1
	xkControlL  = 0xffe3
	xkAltL      = 0xffe9
	xkSuperL    = 0xffeb
	xkDelete    = 0xffff
	xkSpace     = 0x20
)

var namedKeysyms = map[string]uint32{
	"Escape": xkEscape, "Enter": xkReturn, "ArrowLeft": xkLeft, "ArrowRight": xkRight,
	"ArrowUp": xkUp, "ArrowDown": xkDown, "Space": xkSpace, "Tab": xkTab,
	"Backspace": xkBackSpace, "Delete": xkDelete, "Home": xkHome, "End": xkEnd,
	"PageUp": xkPrior, "PageDown": xkNext,
}

// chordKeysym maps a chord key (W3C code) to the keysym we inject: letters (lower case),
// digits, the named keys in [namedKeysyms] and F1-F12. Anything else is not sendable.
func chordKeysym(code string) (uint32, bool) {
	if len(code) == 4 && code[:3] == "Key" {
		c := code[3]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			return uint32(c), true
		}
		return 0, false
	}
	if len(code) == 6 && code[:5] == "Digit" {
		if d := code[5]; d >= '0' && d <= '9' {
			return uint32(d), true
		}
		return 0, false
	}
	if sym, ok := namedKeysyms[code]; ok {
		return sym, true
	}
	if rest, ok := strings.CutPrefix(code, "F"); ok {
		if n, err := strconv.Atoi(rest); err == nil && n >= 1 && n <= 12 && strconv.Itoa(n) == rest {
			return uint32(xkF1 + n - 1), true
		}
	}
	return 0, false
}

// modifierKeysym is the left-hand key of a modifier (Control_L, Alt_L, Shift_L, Super_L for
// Meta).
func modifierKeysym(m platform.KeyModifier) uint32 {
	switch m {
	case platform.Ctrl:
		return xkControlL
	case platform.Alt:
		return xkAltL
	case platform.Shift:
		return xkShiftL
	default:
		return xkSuperL
	}
}

// keysymToKeycode finds a keycode producing sym in a GetKeyboardMapping reply that starts at
// minKeycode, searching column by column like XKeysymToKeycode (so the unshifted key wins).
func keysymToKeycode(minKeycode uint8, perKeycode int, syms []uint32, sym uint32) (uint8, bool) {
	if perKeycode <= 0 {
		return 0, false
	}
	count := len(syms) / perKeycode
	for col := range perKeycode {
		for i := range count {
			if syms[i*perKeycode+col] == sym {
				return uint8(int(minKeycode) + i), true
			}
		}
	}
	return 0, false
}
