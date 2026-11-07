package win

// Game window tracking via WinEvent hooks.
//
// Subscribe starts one OS-thread-locked goroutine that installs out-of-context hooks for
// EVENT_SYSTEM_FOREGROUND and EVENT_OBJECT_LOCATIONCHANGE and runs a message loop (required:
// out-of-context WinEvents are delivered through the installing thread's queue). A thread timer
// rescans for the game window every second so Found/Lost are reported. The hook callback only
// updates that thread's state and pushes events into an unbounded queue.

import (
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

const (
	rescanMS = 1000

	eventSystemForeground     = 0x0003
	eventObjectLocationChange = 0x800B
	winEventOutOfContext      = 0x0000
	objidWindow               = 0
	childidSelf               = 0
	wmTimer                   = 0x0113
)

type windowTracker struct {
	subscribed atomic.Bool
}

var _ platform.WindowTracker = (*windowTracker)(nil)

func (t *windowTracker) GameWindow() (platform.WindowInfo, bool) {
	hwnd, ok := findGameWindow()
	if !ok {
		return platform.WindowInfo{}, false
	}
	return windowInfo(hwnd)
}

func (t *windowTracker) IsGameFocused() bool {
	fg := windows.GetForegroundWindow()
	if fg == 0 {
		return false
	}
	title, ok := windowTitle(fg)
	return ok && titleMatches(title)
}

func (t *windowTracker) Subscribe() (<-chan platform.WindowEvent, error) {
	if t.subscribed.Swap(true) {
		return nil, platform.ErrAlreadyStarted
	}
	q := newQueue[platform.WindowEvent]()
	ready := make(chan error, 1)
	go hookThread(q, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return q.out, nil
}

func hwndID(hwnd windows.HWND) uint64 { return uint64(hwnd) }

// isOwnWindow reports whether hwnd belongs to this process (the settings and price windows,
// hidden helpers).
func isOwnWindow(hwnd windows.HWND) bool {
	var pid uint32
	// A stale HWND leaves pid at 0, which is never our process id.
	_, _ = windows.GetWindowThreadProcessId(hwnd, &pid)
	return pid == windows.GetCurrentProcessId()
}

// windowTitle is the title of another process's window; false for our own windows, which are
// never the game.
//
// Our own windows are skipped because GetWindowTextW on a window of this process that another
// thread owns sends it WM_GETTEXT and waits for the reply. While the game is not running, the
// tracker thread looks at every visible window each second, so it would block on the UI thread,
// and the UI thread waits for the tracker's first scan during startup.
func windowTitle(hwnd windows.HWND) (string, bool) {
	if isOwnWindow(hwnd) {
		return "", false
	}
	var buf [256]uint16
	// SAFETY: buf is a valid writable buffer and we pass its length. Reading another process's
	// window title does not send messages, so this cannot re-enter our hook callback.
	r, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	n := int(int32(r))
	if n <= 0 {
		return "", false
	}
	return windows.UTF16ToString(buf[:min(n, len(buf))]), true
}

// windowRect is the visible window bounds: DWM extended frame bounds (excludes the invisible
// resize border on Windows 10+), falling back to GetWindowRect.
func windowRect(hwnd windows.HWND) (platform.Rect, bool) {
	var r windows.Rect
	// SAFETY: r is a valid RECT and we pass its exact size for DWMWA_EXTENDED_FRAME_BOUNDS.
	err := windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_EXTENDED_FRAME_BOUNDS, unsafe.Pointer(&r), uint32(unsafe.Sizeof(r)))
	if err != nil {
		// SAFETY: r is a valid out-pointer for the call.
		if ok, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r))); ok == 0 {
			return platform.Rect{}, false
		}
	}
	return rectFromLTRB(r.Left, r.Top, r.Right, r.Bottom), true
}

func windowInfo(hwnd windows.HWND) (platform.WindowInfo, bool) {
	if !windows.IsWindow(hwnd) {
		return platform.WindowInfo{}, false
	}
	focused := windows.GetForegroundWindow() == hwnd
	rect, ok := windowRect(hwnd)
	if !ok {
		return platform.WindowInfo{}, false
	}
	title, ok := windowTitle(hwnd)
	if !ok {
		title = platform.GameWindowTitle
	}
	return platform.WindowInfo{Title: title, Rect: rect, Focused: focused, NativeID: hwndID(hwnd)}, true
}

var gameTitleUTF16 = sync.OnceValue(func() *uint16 {
	p, err := windows.UTF16PtrFromString(platform.GameWindowTitle)
	if err != nil {
		panic(err) // a constant without NUL bytes
	}
	return p
})

// EnumWindows state. The callback is created once (callbacks are a limited resource) and
// reports through enumFound under enumMu; EnumWindows calls it synchronously on the calling
// thread, so the lock covers the whole enumeration. Passing a Go pointer through lParam instead
// would need converting the uintptr back to a pointer in the callback.
var (
	enumMu       sync.Mutex
	enumFound    windows.HWND
	enumCallback = sync.OnceValue(func() uintptr { return windows.NewCallback(enumProc) })
)

func enumProc(hwnd windows.HWND, _ uintptr) uintptr {
	if windows.IsWindowVisible(hwnd) {
		if title, ok := windowTitle(hwnd); ok && titleMatches(title) {
			enumFound = hwnd
			return 0 // stop
		}
	}
	return 1
}

func findGameWindow() (windows.HWND, bool) {
	// SAFETY: a null class name and a NUL-terminated title that lives for the process.
	if hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(gameTitleUTF16()))); hwnd != 0 {
		return windows.HWND(hwnd), true
	}
	// Fallback for titles with stray whitespace/padding: enumerate top-level windows.
	enumMu.Lock()
	defer enumMu.Unlock()
	enumFound = 0
	// SAFETY: enumProc matches WNDENUMPROC (two pointer-sized arguments, BOOL result) and does
	// not use lParam. EnumWindows returns an error when the callback stops early, which is how
	// we signal "found".
	_ = windows.EnumWindows(enumCallback(), nil)
	return enumFound, enumFound != 0
}

// hookCtx is one tracker thread's state. The WinEvent callback runs inside that thread's
// GetMessageW, so it is only ever touched from that thread (Rust kept it in a thread_local).
type hookCtx struct {
	q     *queue[platform.WindowEvent]
	state trackerState
}

// hookCtxs maps a tracker thread id to its context, standing in for Rust's thread_local.
var hookCtxs sync.Map // uint32 -> *hookCtx

var winEventCallback = sync.OnceValue(func() uintptr { return windows.NewCallback(winEventProc) })

func currentCtx() *hookCtx {
	if c, ok := hookCtxs.Load(windows.GetCurrentThreadId()); ok {
		return c.(*hookCtx)
	}
	return nil
}

func (c *hookCtx) emit(events ...platform.WindowEvent) {
	for _, ev := range events {
		c.q.push(ev)
	}
}

func (c *hookCtx) rescan() {
	var found *platform.WindowInfo
	if hwnd, ok := findGameWindow(); ok {
		if info, ok := windowInfo(hwnd); ok {
			found = &info
		}
	}
	c.emit(c.state.onScan(found)...)
}

func winEventProc(_ uintptr, event uint32, hwnd windows.HWND, idObject, idChild int32, _, _ uint32) uintptr {
	c := currentCtx()
	if c == nil {
		return 0
	}
	switch event {
	case eventSystemForeground:
		if _, tracked := c.state.gameID(); !tracked {
			// Untracked: the game may just have been launched or focused; don't wait for the timer.
			c.rescan()
		} else if ev, ok := c.state.onForeground(hwndID(hwnd)); ok {
			c.emit(ev)
		}
	case eventObjectLocationChange:
		id, tracked := c.state.gameID()
		if idObject != objidWindow || idChild != childidSelf || !tracked || id != hwndID(hwnd) {
			return 0
		}
		if rect, ok := windowRect(hwnd); ok {
			if ev, ok := c.state.onMoved(id, rect); ok {
				c.emit(ev)
			}
		}
	}
	return 0
}

func setHook(event uint32) (uintptr, error) {
	// SAFETY: winEventProc has the WINEVENTPROC signature and its callback lives for the whole
	// program. WINEVENT_OUTOFCONTEXT means no DLL injection: events are queued to this thread
	// and the callback runs inside this thread's GetMessageW.
	h, _, err := procSetWinEventHook.Call(uintptr(event), uintptr(event), 0, winEventCallback(), 0, 0, winEventOutOfContext)
	return h, err
}

func unhook(hooks []uintptr) {
	for _, h := range hooks {
		if h != 0 {
			// SAFETY: h was returned by SetWinEventHook on this thread and not yet unhooked.
			procUnhookWinEvent.Call(h)
		}
	}
}

func hookThread(q *queue[platform.WindowEvent], ready chan<- error) {
	// The hooks and the timer belong to this OS thread, and their messages arrive on its queue.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tid := windows.GetCurrentThreadId()
	c := &hookCtx{q: q}
	hookCtxs.Store(tid, c)
	defer hookCtxs.Delete(tid)

	var hooks []uintptr
	for _, event := range []uint32{eventSystemForeground, eventObjectLocationChange} {
		h, err := setHook(event)
		if h == 0 {
			unhook(hooks)
			ready <- platform.Backend("SetWinEventHook failed: %v", err)
			return
		}
		hooks = append(hooks, h)
	}
	defer unhook(hooks)
	// SAFETY: a thread timer (no HWND, no TIMERPROC) just posts WM_TIMER to this thread's queue.
	timer, _, _ := procSetTimer.Call(0, 0, rescanMS, 0)
	if timer != 0 {
		// SAFETY: timer was returned by SetTimer on this thread with no HWND.
		defer procKillTimer.Call(0, timer)
	}
	// Report ready before the first scan: Subscribe runs on the UI thread and must not wait on
	// Win32 calls made here.
	ready <- nil
	c.rescan()

	m := new(msg)
	for getMessage(m) {
		if m.message == wmTimer && m.hwnd == 0 {
			c.rescan()
		}
		dispatch(m)
	}
	slog.Warn("window tracker message loop ended")
}
