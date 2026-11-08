package win

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:generate go run golang.org/x/sys/windows/mkwinsyscall -output zsyscall_windows.go user32_windows.go

// user32 calls that golang.org/x/sys/windows does not wrap. Window handles, hooks and timers
// are plain values here; hook and timer callbacks are windows.NewCallback pointers. The //sys
// lines are mkwinsyscall's input, not prose (hence the nolint).
//
//nolint:gocritic
//sys	findWindow(className *uint16, windowName *uint16) (hwnd windows.HWND) = user32.FindWindowW
//sys	getWindowText(hwnd windows.HWND, buf *uint16, size int32) (n int32) = user32.GetWindowTextW
//sys	getWindowRect(hwnd windows.HWND, rect *windows.Rect) (err error) = user32.GetWindowRect
//sys	setWinEventHook(eventMin uint32, eventMax uint32, module windows.Handle, callback uintptr, pid uint32, tid uint32, flags uint32) (hook windows.Handle, err error) = user32.SetWinEventHook
//sys	unhookWinEvent(hook windows.Handle) (ok bool) = user32.UnhookWinEvent
//sys	setTimer(hwnd windows.HWND, id uintptr, ms uint32, callback uintptr) (timer uintptr) = user32.SetTimer
//sys	killTimer(hwnd windows.HWND, id uintptr) (ok bool) = user32.KillTimer
//sys	getMessage(m *msg, hwnd windows.HWND, msgMin uint32, msgMax uint32) (ret int32) = user32.GetMessageW
//sys	translateMessage(m *msg) (ok bool) = user32.TranslateMessage
//sys	dispatchMessage(m *msg) (ret uintptr) = user32.DispatchMessageW
//sys	postThreadMessage(tid uint32, message uint32, wParam uintptr, lParam uintptr) (err error) = user32.PostThreadMessageW
//sys	setWindowsHookEx(id int32, callback uintptr, module windows.Handle, tid uint32) (hook windows.Handle, err error) = user32.SetWindowsHookExW
//sys	callNextHookEx(hook windows.Handle, code int32, wParam uintptr, lParam uintptr) (ret uintptr) = user32.CallNextHookEx
//sys	unhookWindowsHookEx(hook windows.Handle) (err error) = user32.UnhookWindowsHookEx
//sys	sendInput(count uint32, inputs *input, size int32) (sent uint32, err error) = user32.SendInput
//sys	getAsyncKeyState(vk int32) (state int16) = user32.GetAsyncKeyState
//sys	mapVirtualKeyEx(code uint32, mapType uint32, layout windows.Handle) (mapped uint32) = user32.MapVirtualKeyExW
//sys	setWindowPos(hwnd windows.HWND, insertAfter windows.HWND, x int32, y int32, cx int32, cy int32, flags uint32) (err error) = user32.SetWindowPos
//sys	showWindow(hwnd windows.HWND, cmd int32) (wasVisible bool) = user32.ShowWindow
//sys	setLayeredWindowAttributes(hwnd windows.HWND, key uint32, alpha byte, flags uint32) (err error) = user32.SetLayeredWindowAttributes
//sys	getPhysicalCursorPos(pt *point) (err error) = user32.GetPhysicalCursorPos
//sys	getCursorPos(pt *point) (err error) = user32.GetCursorPos

// GetWindowLongPtrW and SetWindowLongPtrW are macros for the 32-bit variants on 32-bit
// Windows, where user32 does not export the Ptr names, so they are resolved by hand.
var (
	procGetWindowLongPtrW = ptrProc("GetWindowLongPtrW", "GetWindowLongW")
	procSetWindowLongPtrW = ptrProc("SetWindowLongPtrW", "SetWindowLongW")
)

func ptrProc(wide, narrow string) *windows.LazyProc {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return moduser32.NewProc(wide)
	}
	return moduser32.NewProc(narrow)
}

// point is POINT.
type point struct{ x, y int32 }

// msg is MSG.
type msg struct {
	hwnd     windows.HWND
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

const (
	wmQuit  = 0x0012
	wmTimer = 0x0113
)

// runMessageLoop runs this thread's message loop until WM_QUIT (see [quitMessageLoop]) or an
// error, calling onMessage (if set) for every message before it is dispatched. Hook callbacks
// run inside it, on this thread.
//
// m must be heap memory reachable from a global (the hook's state): hook callbacks run on this
// goroutine's stack while GetMessageW and DispatchMessageW hold m, and may grow (move) the
// stack, which a stack-allocated m would not survive.
func runMessageLoop(m *msg, onMessage func(*msg)) {
	// GetMessageW returns 0 on WM_QUIT and -1 on error; both end the loop.
	for getMessage(m, 0, 0, 0) > 0 {
		if onMessage != nil {
			onMessage(m)
		}
		translateMessage(m)
		dispatchMessage(m)
	}
}

// quitMessageLoop makes thread tid's [runMessageLoop] return. A thread whose loop already ended
// has no queue left to post to; that error is ignored.
func quitMessageLoop(tid uint32) { _ = postThreadMessage(tid, wmQuit, 0, 0) }
