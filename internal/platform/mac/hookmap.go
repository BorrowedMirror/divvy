package mac

import (
	"math"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Pure half of the passive hook (hook_darwin.go): CGEvent -> [platform.InputEvent] translation
// and the non-blocking hand-off, ported from the shared Rust `backend::hook` and the rdev fork's
// macOS event conversion.

// hookChannelCapacity is how many events are buffered before new ones are dropped (consumer
// stalled).
const hookChannelCapacity = 1024

// CGEventType values (CGEventTypes.h).
const (
	cgEventMouseMoved         uint32 = 5
	cgEventLeftMouseDragged   uint32 = 6
	cgEventRightMouseDragged  uint32 = 7
	cgEventKeyDown            uint32 = 10
	cgEventKeyUp              uint32 = 11
	cgEventFlagsChanged       uint32 = 12
	cgEventScrollWheel        uint32 = 22
	cgEventOtherMouseDragged  uint32 = 27
	cgEventTapDisabledTimeout uint32 = 0xFFFFFFFE
	cgEventTapDisabledInput   uint32 = 0xFFFFFFFF
)

// hookEventMask is the CGEventMask for the tap: key down/up and modifier changes, plus mouse
// moves and the wheel when withMouse is set (Ctrl + wheel stash tab switching).
func hookEventMask(withMouse bool) uint64 {
	mask := uint64(1)<<cgEventKeyDown | uint64(1)<<cgEventKeyUp | uint64(1)<<cgEventFlagsChanged
	if withMouse {
		mask |= uint64(1)<<cgEventMouseMoved | uint64(1)<<cgEventLeftMouseDragged |
			uint64(1)<<cgEventRightMouseDragged | uint64(1)<<cgEventScrollWheel
	}
	return mask
}

// hookOnlyKeys are virtual keycodes the hook reports that injection never sends (rdev's
// macOS table plus its W3C names), on top of [keyTable].
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

// keyName maps a macOS virtual keycode to its W3C KeyboardEvent.code name (the format
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

// cgEvent is the part of a CGEvent the hook reads.
type cgEvent struct {
	Type uint32
	// Keycode is kCGKeyboardEventKeycode (key and flags-changed events).
	Keycode int64
	// Flags is CGEventGetFlags (flags-changed events).
	Flags uint64
	// X, Y is CGEventGetLocation (mouse moves).
	X, Y float64
	// WheelY is kCGScrollWheelEventPointDeltaAxis1 (scroll events).
	WheelY int64
}

// hookState is what the translation remembers between events.
type hookState struct {
	// lastFlags decides whether a flags-changed event is a modifier press or release.
	lastFlags uint64
}

// mapEvent translates a tap event. Mouse buttons and horizontal-only scrolls are dropped.
func (s *hookState) mapEvent(ev cgEvent) (platform.InputEvent, bool) {
	switch ev.Type {
	case cgEventKeyDown, cgEventKeyUp:
		code, ok := keycodeOf(ev.Keycode)
		if !ok {
			return platform.InputEvent{}, false
		}
		typ := platform.KeyDown
		if ev.Type == cgEventKeyUp {
			typ = platform.KeyUp
		}
		return platform.InputEvent{Type: typ, Key: keyName(code)}, true
	case cgEventFlagsChanged:
		code, ok := keycodeOf(ev.Keycode)
		if !ok {
			return platform.InputEvent{}, false
		}
		// As rdev: fewer flags than before means a modifier went up.
		typ := platform.KeyDown
		if ev.Flags < s.lastFlags {
			typ = platform.KeyUp
		}
		s.lastFlags = ev.Flags
		return platform.InputEvent{Type: typ, Key: keyName(code)}, true
	case cgEventMouseMoved, cgEventLeftMouseDragged, cgEventRightMouseDragged, cgEventOtherMouseDragged:
		// rdev reports drags as moves too (its mask includes them).
		return platform.InputEvent{Type: platform.MouseMove, X: saturateI32(ev.X), Y: saturateI32(ev.Y)}, true
	case cgEventScrollWheel:
		if ev.WheelY == 0 {
			return platform.InputEvent{}, false
		}
		return platform.InputEvent{Type: platform.Wheel, Delta: int32(min(max(ev.WheelY, math.MinInt32), math.MaxInt32))}, true
	}
	return platform.InputEvent{}, false
}

func keycodeOf(v int64) (uint16, bool) {
	if v < 0 || v > math.MaxUint16 {
		return 0, false
	}
	return uint16(v), true
}

// saturateI32 truncates toward zero and saturates, like Rust's `as i32`.
func saturateI32(v float64) int32 {
	if math.IsNaN(v) {
		return 0
	}
	return int32(min(max(math.Trunc(v), math.MinInt32), math.MaxInt32))
}

// forward is the non-blocking hand-off; it drops the event when the consumer lags.
func forward(ch chan<- platform.InputEvent, ev platform.InputEvent) bool {
	select {
	case ch <- ev:
		return true
	default:
		return false
	}
}
