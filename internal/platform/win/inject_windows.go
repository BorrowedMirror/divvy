package win

// Chord injection with SendInput.

import (
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

const (
	inputKeyboard        = 1
	keyeventfExtendedKey = 0x0001
	keyeventfKeyUp       = 0x0002
	mapvkVKToVSCEx       = 4
	// eventMarker tags our injected events in dwExtraInfo.
	eventMarker = 100
)

// keybdInput is KEYBDINPUT.
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input is INPUT with the keyboard member of its union. The padding makes it as large as the
// union's largest member (MOUSEINPUT).
type input struct {
	typ     uint32
	ki      keybdInput
	padding [8]byte
}

// SendInput rejects a cbSize other than sizeof(INPUT): 40 bytes on 64-bit, 28 on 32-bit. This
// only compiles while input has exactly that size (the array length must be 0).
var _ [0]struct{} = [unsafe.Sizeof(input{}) - (28 + 3*(unsafe.Sizeof(uintptr(0))-4))]struct{}{}

// sendInputSimulator sends chords with SendInput.
//
// Modifier handling: the user is usually still holding the hotkey (e.g. Ctrl+D) when this runs.
// Chord modifiers already held are not pressed or released by us: releasing Ctrl would leave D
// physically down without Ctrl, so D auto-repeat would stop matching the RegisterHotKey binding
// and reach the game as plain D. Held modifiers that are not part of the chord (e.g. Shift from
// Ctrl+Shift+D) are key-upped first and not restored; restoring would risk a stuck modifier if
// the user let go meanwhile. The key itself is sent by virtual-key code (layout independent).
type sendInputSimulator struct{}

var _ platform.InputSimulator = sendInputSimulator{}

// SendChord implements [platform.InputSimulator].
func (sendInputSimulator) SendChord(chord platform.Chord) error {
	vk, ok := vkForCode(chord.Key)
	if !ok {
		return platform.InvalidChord(chord.String())
	}
	return platform.RunChord(chord.Modifiers, heldModifiers(), sendKeys{vk: vk})
}

func heldModifiers() []platform.KeyModifier {
	var held []platform.KeyModifier
	for _, m := range []platform.KeyModifier{platform.Ctrl, platform.Alt, platform.Shift, platform.Meta} {
		if getAsyncKeyState(int32(modifierVK(m))) < 0 { // high bit set: key is down
			held = append(held, m)
		}
	}
	return held
}

// sendKeys implements [platform.ChordKeys] over SendInput.
type sendKeys struct {
	vk uint16
}

var _ platform.ChordKeys = sendKeys{}

func (k sendKeys) ReleaseHeld(m platform.KeyModifier) error { return k.Release(m) }
func (k sendKeys) Press(m platform.KeyModifier) error       { return sendKey(modifierVK(m), true, false) }
func (k sendKeys) Release(m platform.KeyModifier) error     { return sendKey(modifierVK(m), false, true) }
func (k sendKeys) Click() error                             { return sendKey(k.vk, true, true) }

// sendKey injects a key-down, a key-up, or both in one SendInput call, carrying both the
// virtual key and its scan code for the foreground window's layout (games that read scan codes
// see the right key).
func sendKey(vk uint16, down, up bool) error {
	scan := scanCode(vk)
	var flags uint32
	if isExtendedVK(vk) {
		flags |= keyeventfExtendedKey
	}
	var events []input
	if down {
		events = append(events, keyInput(vk, scan, flags))
	}
	if up {
		events = append(events, keyInput(vk, scan, flags|keyeventfKeyUp))
	}
	sent, err := sendInput(uint32(len(events)), &events[0], int32(unsafe.Sizeof(events[0])))
	if int(sent) != len(events) {
		if err != nil {
			return platform.Backend("SendInput: sent %d of %d events (blocked by UIPI?): %w", sent, len(events), err)
		}
		return platform.Backend("SendInput: sent %d of %d events (blocked by UIPI?)", sent, len(events))
	}
	return nil
}

func keyInput(vk, scan uint16, flags uint32) input {
	return input{typ: inputKeyboard, ki: keybdInput{wVk: vk, wScan: scan, dwFlags: flags, dwExtraInfo: eventMarker}}
}

// scanCode maps vk to its (extended) scan code in the foreground window's keyboard layout; 0
// when there is no mapping.
func scanCode(vk uint16) uint16 {
	var tid uint32
	if fg := windows.GetForegroundWindow(); fg != 0 {
		tid, _ = windows.GetWindowThreadProcessId(fg, nil)
	}
	return uint16(mapVirtualKeyEx(uint32(vk), mapvkVKToVSCEx, windows.GetKeyboardLayout(tid)))
}
