package win

// Game window tracking via WinEvent hooks.
//
// Subscribe starts one OS-thread-locked goroutine that installs out-of-context hooks for
// EVENT_SYSTEM_FOREGROUND and EVENT_OBJECT_LOCATIONCHANGE and runs a message loop (required:
// out-of-context WinEvents are delivered through the installing thread's queue). A thread timer
// rescans for the game window every second so Found/Lost are reported. The hook callback only
// updates that thread's state and pushes events into an unbounded queue. When the subscriber's
// context ends, WM_QUIT stops the loop, the hooks and timer are removed and the channel closes.

import (
	"context"
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
)

type windowTracker struct{}

var _ platform.WindowTracker = windowTracker{}

// GameWindow implements [platform.WindowTracker].
func (windowTracker) GameWindow() (platform.WindowInfo, bool) {
	hwnd, ok := findGameWindow()
	if !ok {
		return platform.WindowInfo{}, false
	}
	return windowInfo(hwnd)
}

// IsGameFocused implements [platform.WindowTracker].
func (windowTracker) IsGameFocused() bool {
	fg := windows.GetForegroundWindow()
	if fg == 0 {
		return false
	}
	title, ok := windowTitle(fg)
	return ok && titleMatches(title)
}

// Subscribe implements [platform.WindowTracker]. One tracker thread runs per process: a second
// call while one runs returns [platform.ErrAlreadyStarted]; once ctx is done and the channel
// closed, Subscribe may be called again.
func (windowTracker) Subscribe(ctx context.Context) (<-chan platform.WindowEvent, error) {
	c := &trackerThread{}
	if !activeTracker.CompareAndSwap(nil, c) {
		return nil, platform.ErrAlreadyStarted
	}
	ready := make(chan error, 1)
	go c.run(ctx, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return c.q.out, nil
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
	// Reading another process's window title does not send messages, so this cannot re-enter
	// our hook callback.
	n := int(getWindowText(hwnd, &buf[0], int32(len(buf))))
	if n <= 0 {
		return "", false
	}
	return windows.UTF16ToString(buf[:min(n, len(buf))]), true
}

// windowRect is the visible window bounds: DWM extended frame bounds (excludes the invisible
// resize border on Windows 10+), falling back to GetWindowRect.
func windowRect(hwnd windows.HWND) (platform.Rect, bool) {
	var r windows.Rect
	err := windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_EXTENDED_FRAME_BOUNDS, unsafe.Pointer(&r), uint32(unsafe.Sizeof(r)))
	if err != nil && getWindowRect(hwnd, &r) != nil {
		return platform.Rect{}, false
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

// gameTitleUTF16 is the NUL-terminated game title, kept for the life of the process.
var gameTitleUTF16 = sync.OnceValues(func() (*uint16, error) {
	return windows.UTF16PtrFromString(platform.GameWindowTitle)
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
	if title, err := gameTitleUTF16(); err == nil {
		if hwnd := findWindow(nil, title); hwnd != 0 {
			return hwnd, true
		}
	}
	// Fallback for titles with stray whitespace/padding: enumerate top-level windows.
	enumMu.Lock()
	defer enumMu.Unlock()
	enumFound = 0
	// enumProc matches WNDENUMPROC (two pointer-sized arguments, BOOL result) and does not use
	// lParam. EnumWindows returns an error when the callback stops early, which is how we
	// signal "found".
	_ = windows.EnumWindows(enumCallback(), nil)
	return enumFound, enumFound != 0
}

// trackerThread is the running tracker thread's state. The WinEvent callback runs inside that
// thread's GetMessageW, so state and q are only touched from that thread (q.push is safe from
// anywhere anyway).
type trackerThread struct {
	q     *queue[platform.WindowEvent]
	state trackerState
	// msg is the message loop's MSG; see [runMessageLoop].
	msg msg
}

// activeTracker is the one running tracker thread, nil when none runs. Out-of-context WinEvent
// callbacks carry no user data, so they find their state here.
var activeTracker atomic.Pointer[trackerThread]

var winEventCallback = sync.OnceValue(func() uintptr { return windows.NewCallback(winEventProc) })

func (c *trackerThread) emit(events ...platform.WindowEvent) {
	for _, ev := range events {
		c.q.push(ev)
	}
}

func (c *trackerThread) rescan() {
	var found *platform.WindowInfo
	if hwnd, ok := findGameWindow(); ok {
		if info, ok := windowInfo(hwnd); ok {
			found = &info
		}
	}
	c.emit(c.state.onScan(found)...)
}

func winEventProc(_ windows.Handle, event uint32, hwnd windows.HWND, idObject, idChild int32, _, _ uint32) uintptr {
	c := activeTracker.Load()
	if c == nil || c.q == nil {
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

// run is the tracker thread. It reports on ready whether the hooks are installed; on success
// it serves until ctx is done (or the message loop fails), then releases everything and closes
// the event channel.
func (c *trackerThread) run(ctx context.Context, ready chan<- error) {
	// The hooks and the timer belong to this OS thread, and their messages arrive on its queue.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Deferred calls run last-in first-out: the channel closes only after the hooks are gone
	// and the tracker slot is free, so the consumer may subscribe again right away.
	defer func() {
		if c.q != nil {
			c.q.close()
		}
	}()
	defer activeTracker.CompareAndSwap(c, nil)

	var hooks []windows.Handle
	defer func() {
		for _, h := range hooks {
			unhookWinEvent(h)
		}
	}()
	for _, event := range []uint32{eventSystemForeground, eventObjectLocationChange} {
		// WINEVENT_OUTOFCONTEXT means no DLL injection: events are queued to this thread and the
		// callback runs inside this thread's GetMessageW.
		h, err := setWinEventHook(event, event, 0, winEventCallback(), 0, 0, winEventOutOfContext)
		if err != nil {
			ready <- platform.Backend("SetWinEventHook: %w", err)
			return
		}
		hooks = append(hooks, h)
	}
	// A thread timer (no HWND, no TIMERPROC) just posts WM_TIMER to this thread's queue.
	if timer := setTimer(0, 0, rescanMS, 0); timer != 0 {
		defer killTimer(0, timer)
	}

	c.q = newQueue[platform.WindowEvent](ctx)
	tid := windows.GetCurrentThreadId()
	defer context.AfterFunc(ctx, func() { quitMessageLoop(tid) })()
	// Report ready before the first scan: Subscribe runs on the UI thread and must not wait on
	// Win32 calls made here.
	ready <- nil
	c.rescan()

	runMessageLoop(&c.msg, func(m *msg) {
		if m.message == wmTimer && m.hwnd == 0 {
			c.rescan()
		}
	})
	if ctx.Err() == nil {
		slog.Warn("window tracker message loop ended")
	}
}
