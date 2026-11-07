package win

// Passive input hook (WH_KEYBOARD_LL, plus WH_MOUSE_LL when asked) and SendInput chord
// injection.

import (
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// The pure key mapping uses its own constants; pin them to x/sys/windows' at compile time (an
// index out of range of a one-element array is a compile error unless the difference is 0).
var (
	_ = [1]struct{}{}[vkShift-windows.VK_SHIFT]
	_ = [1]struct{}{}[vkControl-windows.VK_CONTROL]
	_ = [1]struct{}{}[vkMenu-windows.VK_MENU]
	_ = [1]struct{}{}[vkLWin-windows.VK_LWIN]
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	hcAction     = 0

	inputKeyboard        = 1
	keyeventfExtendedKey = 0x0001
	keyeventfKeyUp       = 0x0002
	mapvkVKToVSCEx       = 4
	// eventMarker tags our injected events in dwExtraInfo, as enigo did (its EVENT_MARKER).
	eventMarker = 100
)

// kbdllHookStruct is KBDLLHOOKSTRUCT.
type kbdllHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

// msllHookStruct is MSLLHOOKSTRUCT.
type msllHookStruct struct {
	x, y        int32
	mouseData   uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

// The ONE process-wide passive hook. hookStarted is true while a hook thread is running (or
// starting); it is cleared when the hook fails to install so a later Start can retry.
var (
	hookStarted atomic.Bool
	// hookTx is written by the hook thread before it installs the hooks and only read by the
	// hook callbacks, which run on that same thread inside its GetMessageW.
	hookTx chan<- platform.InputEvent

	keyboardCallback = sync.OnceValue(func() uintptr { return windows.NewCallback(keyboardProc) })
	mouseCallback    = sync.OnceValue(func() uintptr { return windows.NewCallback(mouseProc) })
)

type inputListener struct {
	hookMouse bool
}

var _ platform.InputListener = (*inputListener)(nil)

// Start installs the global hook on its own thread and never blocks. A failure to install is
// logged and reported by the returned channel closing; the hook can then be started again.
func (l *inputListener) Start() (<-chan platform.InputEvent, error) {
	if hookStarted.Swap(true) {
		return nil, platform.ErrAlreadyStarted
	}
	tx := make(chan platform.InputEvent, hookChannelCapacity)
	go runHook(tx, l.hookMouse)
	return tx, nil
}

// keyboardProc is the LowLevelKeyboardProc. It must return quickly (Windows silently removes a
// low-level hook that exceeds LowLevelHooksTimeout), so it only does a non-blocking send.
func keyboardProc(nCode int32, wParam uintptr, lParam *kbdllHookStruct) uintptr {
	if nCode == hcAction {
		if ev, ok := keyboardEvent(wParam, lParam.vkCode, lParam.scanCode); ok {
			forward(hookTx, ev)
		}
	}
	// SAFETY: passes the hook arguments through unchanged; lParam is the system's
	// KBDLLHOOKSTRUCT, valid for this call.
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, uintptr(unsafe.Pointer(lParam)))
	return r
}

// mouseProc is the LowLevelMouseProc; see keyboardProc.
func mouseProc(nCode int32, wParam uintptr, lParam *msllHookStruct) uintptr {
	if nCode == hcAction {
		if ev, ok := mouseEvent(wParam, lParam.x, lParam.y, lParam.mouseData); ok {
			forward(hookTx, ev)
		}
	}
	// SAFETY: passes the hook arguments through unchanged; lParam is the system's
	// MSLLHOOKSTRUCT, valid for this call.
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, uintptr(unsafe.Pointer(lParam)))
	return r
}

func setWindowsHook(id int, callback uintptr) (uintptr, error) {
	// SAFETY: callback has the HOOKPROC signature (int, WPARAM, pointer LPARAM) and lives for
	// the whole program. Low-level hooks are not injected into other processes: the system
	// calls them on this thread, inside its GetMessageW, so they need no module handle.
	h, _, err := procSetWindowsHookExW.Call(uintptr(id), callback, 0, 0)
	return h, err
}

func runHook(tx chan platform.InputEvent, withMouse bool) {
	// Low-level hook callbacks are delivered to the installing thread's message loop.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer hookStarted.Store(false)
	// Closed only after the hooks are gone (deferred calls run last-in first-out), so no callback
	// can send on it afterwards.
	defer close(tx)

	hookTx = tx
	var hooks []uintptr
	defer func() {
		for _, h := range hooks {
			// SAFETY: h was returned by SetWindowsHookExW on this thread and not yet removed.
			procUnhookWindowsHookEx.Call(h)
		}
	}()
	kb, err := setWindowsHook(whKeyboardLL, keyboardCallback())
	if kb == 0 {
		slog.Warn("input hook failed to start", "hook", "keyboard", "error", err)
		return
	}
	hooks = append(hooks, kb)
	if withMouse {
		ms, err := setWindowsHook(whMouseLL, mouseCallback())
		if ms == 0 {
			slog.Warn("input hook failed to start", "hook", "mouse", "error", err)
			return
		}
		hooks = append(hooks, ms)
	}

	m := new(msg)
	for getMessage(m) {
		dispatch(m)
	}
	slog.Warn("input hook message loop ended")
}

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
		// SAFETY: GetAsyncKeyState takes any virtual-key code and has no side effects.
		r, _, _ := procGetAsyncKeyState.Call(uintptr(modifierVK(m)))
		if int16(r) < 0 { // high bit set: key is down
			held = append(held, m)
		}
	}
	return held
}

// keybdInput is KEYBDINPUT.
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// input is INPUT with the keyboard member of its union. The padding makes it as large as the
// union's largest member (MOUSEINPUT): 40 bytes on 64-bit, 28 on 32-bit, as SendInput checks.
type input struct {
	typ     uint32
	ki      keybdInput
	padding [8]byte
}

// SendInput rejects a cbSize other than sizeof(INPUT): 40 bytes on 64-bit, 28 on 32-bit.
var _ = [1]struct{}{}[unsafe.Sizeof(input{})-(28+3*(unsafe.Sizeof(uintptr(0))-4))]

// sendKeys implements [platform.ChordKeys] over SendInput.
type sendKeys struct {
	vk uint16
}

func (k sendKeys) ReleaseHeld(m platform.KeyModifier) error { return k.Release(m) }
func (k sendKeys) Press(m platform.KeyModifier) error       { return sendKey(modifierVK(m), true, false) }
func (k sendKeys) Release(m platform.KeyModifier) error     { return sendKey(modifierVK(m), false, true) }
func (k sendKeys) Click() error                             { return sendKey(k.vk, true, true) }

// sendKey injects a key-down, a key-up, or both in one SendInput call, carrying both the
// virtual key and its scan code for the foreground window's layout (games that read scan codes
// see the right key), as enigo did.
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
	// SAFETY: events is a non-empty slice of INPUT-sized structs and we pass its length and the
	// element size.
	n, _, err := procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if int(uint32(n)) != len(events) {
		return platform.Backend("SendInput: not all input events were sent; they may have been blocked by UIPI: %v", err)
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
	// SAFETY: GetKeyboardLayout takes any thread id (0 is the calling thread) and has no side
	// effects.
	layout, _, _ := procGetKeyboardLayout.Call(uintptr(tid))
	// SAFETY: MapVirtualKeyExW is a pure lookup.
	scan, _, _ := procMapVirtualKeyExW.Call(uintptr(vk), mapvkVKToVSCEx, layout)
	return uint16(scan)
}
