package win

import (
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// user32 calls that golang.org/x/sys/windows does not wrap.
var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procFindWindowW                = user32.NewProc("FindWindowW")
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procSetWinEventHook            = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent             = user32.NewProc("UnhookWinEvent")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procSetWindowsHookExW          = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx             = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx        = user32.NewProc("UnhookWindowsHookEx")
	procSendInput                  = user32.NewProc("SendInput")
	procGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	procMapVirtualKeyExW           = user32.NewProc("MapVirtualKeyExW")
	procGetKeyboardLayout          = user32.NewProc("GetKeyboardLayout")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetPhysicalCursorPos       = user32.NewProc("GetPhysicalCursorPos")
	procGetCursorPos               = user32.NewProc("GetCursorPos")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	// GetWindowLongPtrW/SetWindowLongPtrW are macros for the 32-bit variants on 32-bit Windows,
	// where user32 does not export the Ptr names.
	procGetWindowLongPtrW = ptrProc("GetWindowLongPtrW", "GetWindowLongW")
	procSetWindowLongPtrW = ptrProc("SetWindowLongPtrW", "SetWindowLongW")
)

func ptrProc(wide, narrow string) *windows.LazyProc {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return user32.NewProc(wide)
	}
	return user32.NewProc(narrow)
}

// msg is the Win32 MSG structure.
type msg struct {
	hwnd     windows.HWND
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       struct{ x, y int32 }
	lPrivate uint32
}

// getMessage wraps GetMessageW for this thread's queue (any window). It returns false on
// WM_QUIT (0) and on error (-1); both end a message loop.
func getMessage(m *msg) bool {
	// SAFETY: m points to a MSG-sized struct the call fills in; LazyProc.Call is
	// go:uintptrescapes, so m is heap-allocated and does not move while Go callbacks (hook
	// procs dispatched inside GetMessageW) grow this goroutine's stack.
	r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r) > 0
}

// dispatch translates and dispatches a message filled in by getMessage.
func dispatch(m *msg) {
	// SAFETY: m was filled in by GetMessageW.
	procTranslateMessage.Call(uintptr(unsafe.Pointer(m)))
	// SAFETY: as above.
	procDispatchMessageW.Call(uintptr(unsafe.Pointer(m)))
}

// New builds the Windows backend parts. Nothing here can fail at construction: the hooks are
// installed by Subscribe and Start.
func New(opts ...Option) (platform.Parts, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return platform.Parts{
		Kind:           platform.BackendWindows,
		Capabilities:   capabilities(),
		WindowTracker:  &windowTracker{},
		InputListener:  &inputListener{hookMouse: o.hookMouse},
		InputSimulator: sendInputSimulator{},
		Overlay:        newOverlay,
		Cursor:         cursor,
		// Nil, as in the Rust build: Windows gives focus back to the previous window when the
		// panel hides, and a forced SetForegroundWindow can be refused by the foreground lock
		// and only flash the game's taskbar button. activateWindow stays for a later opt-in.
		ActivateWindow: nil,
	}, nil
}

// cursor is the mouse position in physical screen pixels. GetPhysicalCursorPos is not
// DPI-virtualized, so it is right even if the process is not per-monitor DPI aware (for an
// aware process it equals GetCursorPos, the fallback).
func cursor() (x, y float64, ok bool) {
	var pt struct{ x, y int32 }
	// SAFETY: pt is a POINT-sized out-pointer for the call.
	r, _, _ := procGetPhysicalCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if r == 0 {
		// SAFETY: as above.
		if r, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
			return 0, 0, false
		}
	}
	return float64(pt.x), float64(pt.y), true
}

// activateWindow puts another top-level window (the game) in the foreground. The Rust app never
// did this on Windows: when the overlay hides, Windows itself reactivates the previously active
// window, and the card is WS_EX_NOACTIVATE so it never took focus. This is a plain
// SetForegroundWindow with no AttachThreadInput / AllowSetForegroundWindow / synthetic-Alt
// tricks to get around the foreground lock, so it only succeeds while we are allowed to set
// the foreground (we, or the target, are foreground), and otherwise fails harmlessly.
func activateWindow(nativeID uint64) error {
	hwnd := windows.HWND(nativeID)
	if !windows.IsWindow(hwnd) {
		return platform.Backend("window %#x is not a window", nativeID)
	}
	if windows.GetForegroundWindow() == hwnd {
		return nil
	}
	// SAFETY: hwnd was validated; SetForegroundWindow takes any top-level HWND.
	if r, _, err := procSetForegroundWindow.Call(uintptr(hwnd)); r == 0 {
		return platform.Backend("SetForegroundWindow refused (foreground lock): %v", err)
	}
	return nil
}

func newOverlay(native unsafe.Pointer) (platform.OverlayWindow, error) {
	hwnd, err := hwndFrom(native)
	if err != nil {
		return nil, err
	}
	return &overlay{hwnd: windows.HWND(hwnd)}, nil
}
