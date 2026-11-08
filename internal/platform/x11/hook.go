package x11

import (
	"encoding/binary"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Core event codes in a RECORD device-event range.
const (
	evKeyPress      = 2
	evKeyRelease    = 3
	evButtonPress   = 4
	evButtonRelease = 5
	evMotionNotify  = 6
)

// mapEvent translates a recorded core device event. Mouse buttons are not needed by the app and
// are dropped; buttons 4/5 are the vertical wheel (X has no horizontal wheel events).
func mapEvent(typ, detail uint8, rootX, rootY int16) (platform.InputEvent, bool) {
	switch typ & 0x7f {
	case evKeyPress:
		return platform.InputEvent{Type: platform.KeyDown, Key: keyCode(detail)}, true
	case evKeyRelease:
		return platform.InputEvent{Type: platform.KeyUp, Key: keyCode(detail)}, true
	case evButtonPress:
		switch detail {
		case 4:
			return platform.InputEvent{Type: platform.Wheel, Delta: 1}, true
		case 5:
			return platform.InputEvent{Type: platform.Wheel, Delta: -1}, true
		}
	case evMotionNotify:
		return platform.InputEvent{Type: platform.MouseMove, X: int32(rootX), Y: int32(rootY)}, true
	}
	return platform.InputEvent{}, false
}

// recordEvents decodes the data of a RECORD FromServer reply (element header 0: bare 32-byte
// wire events, no timestamps) into input events.
func recordEvents(data []byte, emit func(platform.InputEvent)) {
	for len(data) >= 32 {
		ev := data[:32]
		data = data[32:]
		// KeyPress/ButtonPress/MotionNotify share the layout: type, detail, sequence, time,
		// root, event, child, root-x (20), root-y (22).
		rootX := int16(binary.LittleEndian.Uint16(ev[20:]))
		rootY := int16(binary.LittleEndian.Uint16(ev[22:]))
		if out, ok := mapEvent(ev[0], ev[1], rootX, rootY); ok {
			emit(out)
		}
	}
}

// recordRangeLast is the last core event in the recorded device-event range: key events only,
// unless the mouse is wanted too (Ctrl + wheel stash tab switching), which also records
// pointer motion and buttons and so costs every mouse move a round through the hook.
func recordRangeLast(mouse bool) uint8 {
	if mouse {
		return evMotionNotify
	}
	return evKeyRelease
}
