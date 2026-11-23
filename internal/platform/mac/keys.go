package mac

import "github.com/BorrowedMirror/divvy/internal/platform"

// Key tables: W3C KeyboardEvent.code names <-> macOS virtual keycodes (kVK_*, Carbon
// Events.h). Virtual keycodes are layout-independent (they name physical positions), exactly
// like W3C codes, so injection and the hook share one table.

type keyEntry struct {
	code    string
	keycode uint16
}

// keyTable is (W3C code, kVK_* value).
var keyTable = []keyEntry{
	{"KeyA", 0x00},
	{"KeyS", 0x01},
	{"KeyD", 0x02},
	{"KeyF", 0x03},
	{"KeyH", 0x04},
	{"KeyG", 0x05},
	{"KeyZ", 0x06},
	{"KeyX", 0x07},
	{"KeyC", 0x08},
	{"KeyV", 0x09},
	{"IntlBackslash", 0x0A},
	{"KeyB", 0x0B},
	{"KeyQ", 0x0C},
	{"KeyW", 0x0D},
	{"KeyE", 0x0E},
	{"KeyR", 0x0F},
	{"KeyY", 0x10},
	{"KeyT", 0x11},
	{"Digit1", 0x12},
	{"Digit2", 0x13},
	{"Digit3", 0x14},
	{"Digit4", 0x15},
	{"Digit6", 0x16},
	{"Digit5", 0x17},
	{"Equal", 0x18},
	{"Digit9", 0x19},
	{"Digit7", 0x1A},
	{"Minus", 0x1B},
	{"Digit8", 0x1C},
	{"Digit0", 0x1D},
	{"BracketRight", 0x1E},
	{"KeyO", 0x1F},
	{"KeyU", 0x20},
	{"BracketLeft", 0x21},
	{"KeyI", 0x22},
	{"KeyP", 0x23},
	{"Enter", 0x24},
	{"KeyL", 0x25},
	{"KeyJ", 0x26},
	{"Quote", 0x27},
	{"KeyK", 0x28},
	{"Semicolon", 0x29},
	{"Backslash", 0x2A},
	{"Comma", 0x2B},
	{"Slash", 0x2C},
	{"KeyN", 0x2D},
	{"KeyM", 0x2E},
	{"Period", 0x2F},
	{"Tab", 0x30},
	{"Space", 0x31},
	{"Backquote", 0x32},
	{"Backspace", 0x33},
	{"Escape", 0x35},
	{"MetaRight", 0x36},
	{"MetaLeft", 0x37},
	{"ShiftLeft", 0x38},
	{"CapsLock", 0x39},
	{"AltLeft", 0x3A},
	{"ControlLeft", 0x3B},
	{"ShiftRight", 0x3C},
	{"AltRight", 0x3D},
	{"ControlRight", 0x3E},
	{"Fn", 0x3F},
	{"F17", 0x40},
	{"NumpadDecimal", 0x41},
	{"NumpadMultiply", 0x43},
	{"NumpadAdd", 0x45},
	{"NumLock", 0x47},
	{"NumpadDivide", 0x4B},
	{"NumpadEnter", 0x4C},
	{"NumpadSubtract", 0x4E},
	{"F18", 0x4F},
	{"F19", 0x50},
	{"NumpadEqual", 0x51},
	{"Numpad0", 0x52},
	{"Numpad1", 0x53},
	{"Numpad2", 0x54},
	{"Numpad3", 0x55},
	{"Numpad4", 0x56},
	{"Numpad5", 0x57},
	{"Numpad6", 0x58},
	{"Numpad7", 0x59},
	{"F20", 0x5A},
	{"Numpad8", 0x5B},
	{"Numpad9", 0x5C},
	{"F5", 0x60},
	{"F6", 0x61},
	{"F7", 0x62},
	{"F3", 0x63},
	{"F8", 0x64},
	{"F9", 0x65},
	{"F11", 0x67},
	{"F13", 0x69},
	{"F16", 0x6A},
	{"F14", 0x6B},
	{"F10", 0x6D},
	{"F12", 0x6F},
	{"F15", 0x71},
	// kVK_Help sits where Insert is on PC keyboards.
	{"Insert", 0x72},
	{"Home", 0x73},
	{"PageUp", 0x74},
	{"Delete", 0x75},
	{"F4", 0x76},
	{"End", 0x77},
	{"F2", 0x78},
	{"PageDown", 0x79},
	{"F1", 0x7A},
	{"ArrowLeft", 0x7B},
	{"ArrowRight", 0x7C},
	{"ArrowDown", 0x7D},
	{"ArrowUp", 0x7E},
}

// hookOnlyKeys are virtual keycodes the hook reports that injection never sends, on top of
// [keyTable].
var hookOnlyKeys = map[uint16]string{
	0x48: "AudioVolumeUp",
	0x49: "AudioVolumeDown",
	0x4A: "AudioVolumeMute",
	0x5D: "IntlYen",
	0x5E: "IntlRo",
	0x5F: "NumpadComma",
	0x66: "Lang2",
	0x68: "Lang1",
	0x6E: "ContextMenu",
}

// macKeycode returns the virtual keycode for a W3C code, e.g. `KeyC` -> 0x08.
func macKeycode(code string) (uint16, bool) {
	for _, e := range keyTable {
		if e.code == code {
			return e.keycode, true
		}
	}
	return 0, false
}

// codeForKeycode returns the W3C code for a virtual keycode in [keyTable].
func codeForKeycode(keycode uint16) (string, bool) {
	for _, e := range keyTable {
		if e.keycode == keycode {
			return e.code, true
		}
	}
	return "", false
}

// keyName maps a virtual keycode to its W3C KeyboardEvent.code name (the format
// [platform.Chord] uses); unmapped codes are "Unidentified".
func keyName(keycode uint16) string {
	if code, ok := codeForKeycode(keycode); ok {
		return code
	}
	if code, ok := hookOnlyKeys[keycode]; ok {
		return code
	}
	return "Unidentified"
}

// kCGEventFlagMask* bits (CGEventTypes.h) for the modifiers we model.
const (
	flagShift     uint64 = 0x0002_0000
	flagControl   uint64 = 0x0004_0000
	flagAlternate uint64 = 0x0008_0000
	flagCommand   uint64 = 0x0010_0000
)

// heldFromFlags returns the modifiers held according to CGEventSourceFlagsState bits (Option =
// Alt, Command = Meta).
func heldFromFlags(flags uint64) []platform.KeyModifier {
	var held []platform.KeyModifier
	for _, e := range []struct {
		bit uint64
		mod platform.KeyModifier
	}{
		{flagControl, platform.Ctrl},
		{flagAlternate, platform.Alt},
		{flagShift, platform.Shift},
		{flagCommand, platform.Meta},
	} {
		if flags&e.bit != 0 {
			held = append(held, e.mod)
		}
	}
	return held
}

// modifierKey is how a chord modifier is injected: its left-hand virtual keycode and the event
// flags a key event carries while it is down (the device-independent kCGEventFlagMask* bit plus
// the NX_DEVICEL*KEYMASK bit a real keyboard sets).
type modifierKey struct {
	keycode uint16
	flags   uint64
}

// modifierKeys maps chord modifiers to kVK_Control, kVK_Option, kVK_Shift and kVK_Command.
var modifierKeys = map[platform.KeyModifier]modifierKey{
	platform.Ctrl:  {0x3B, flagControl | 0x0001},
	platform.Alt:   {0x3A, flagAlternate | 0x0020},
	platform.Shift: {0x38, flagShift | 0x0002},
	platform.Meta:  {0x37, flagCommand | 0x0008},
}

// injectBaseFlags are the flags of every injected key event: kCGEventFlagMaskNonCoalesced and
// 0x20000000, which real keyboard events carry.
const injectBaseFlags uint64 = 0x0000_0100 | 0x2000_0000

// injectFlags is the flag word for injected events while mods are down.
func injectFlags(mods []platform.KeyModifier) uint64 {
	flags := injectBaseFlags
	for _, m := range mods {
		flags |= modifierKeys[m].flags
	}
	return flags
}
