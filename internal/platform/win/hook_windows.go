package win

// The passive input hook: WH_KEYBOARD_LL, plus WH_MOUSE_LL when asked, on a message-loop thread
// of its own.

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
	whKeyboardLL = 13
	whMouseLL    = 14
	hcAction     = 0
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
	pt          point
	mouseData   uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

// hookThread is the running hook's state. The hook callbacks run on its thread, inside its
// GetMessageW.
type hookThread struct {
	tx chan<- platform.InputEvent
	// msg is the message loop's MSG; see [runMessageLoop].
	msg msg
}

var (
	// hookStarted is true while the ONE process-wide passive hook runs (or starts).
	hookStarted atomic.Bool
	// activeHook is the running hook the callbacks feed; nil when none runs.
	activeHook atomic.Pointer[hookThread]

	keyboardCallback = sync.OnceValue(func() uintptr { return windows.NewCallback(keyboardProc) })
	mouseCallback    = sync.OnceValue(func() uintptr { return windows.NewCallback(mouseProc) })
)

type inputListener struct {
	hookMouse bool
}

var _ platform.InputListener = inputListener{}

// Start implements [platform.InputListener]. It installs the global hook on its own thread and
// never blocks: a failure to install is logged and reported by the channel closing. When ctx is
// done the hook is removed and the channel closed; either way the hook can be started again.
// Events are buffered ([platform.InputBufferSize]) and dropped while the consumer lags.
func (l inputListener) Start(ctx context.Context) (<-chan platform.InputEvent, error) {
	if !hookStarted.CompareAndSwap(false, true) {
		return nil, platform.ErrAlreadyStarted
	}
	tx := make(chan platform.InputEvent, platform.InputBufferSize)
	go runHook(ctx, &hookThread{tx: tx}, l.hookMouse)
	return tx, nil
}

// keyboardProc is the LowLevelKeyboardProc. It must return quickly (Windows silently removes a
// low-level hook that exceeds LowLevelHooksTimeout), so it only does a non-blocking send.
func keyboardProc(nCode int32, wParam uintptr, lParam *kbdllHookStruct) uintptr {
	if h := activeHook.Load(); h != nil && nCode == hcAction {
		if ev, ok := keyboardEvent(wParam, lParam.vkCode, lParam.scanCode); ok {
			platform.TrySend(h.tx, ev)
		}
	}
	// lParam is the system's KBDLLHOOKSTRUCT, valid for this call.
	return callNextHookEx(0, nCode, wParam, uintptr(unsafe.Pointer(lParam)))
}

// mouseProc is the LowLevelMouseProc; see keyboardProc.
func mouseProc(nCode int32, wParam uintptr, lParam *msllHookStruct) uintptr {
	if h := activeHook.Load(); h != nil && nCode == hcAction {
		if ev, ok := mouseEvent(wParam, lParam.pt.x, lParam.pt.y, lParam.mouseData); ok {
			platform.TrySend(h.tx, ev)
		}
	}
	// lParam is the system's MSLLHOOKSTRUCT, valid for this call.
	return callNextHookEx(0, nCode, wParam, uintptr(unsafe.Pointer(lParam)))
}

// runHook installs the hooks and serves them until ctx is done (or the message loop fails).
func runHook(ctx context.Context, h *hookThread, withMouse bool) {
	// Low-level hook callbacks are delivered to the installing thread's message loop.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Deferred calls run last-in first-out: the hooks go first, then the process-wide slot is
	// freed, then the channel closes, so no callback can send on it afterwards and the consumer
	// may start a new hook as soon as it sees the close.
	defer close(h.tx)
	defer hookStarted.Store(false)
	defer activeHook.Store(nil)
	activeHook.Store(h)

	var hooks []windows.Handle
	defer func() {
		for _, hook := range hooks {
			_ = unhookWindowsHookEx(hook)
		}
	}()
	install := func(name string, id int32, callback uintptr) bool {
		// Low-level hooks are not injected into other processes: the system calls them on this
		// thread, inside its GetMessageW, so they need no module handle.
		hook, err := setWindowsHookEx(id, callback, 0, 0)
		if err != nil {
			slog.Warn("input hook failed to start", "hook", name, "error", err)
			return false
		}
		hooks = append(hooks, hook)
		return true
	}
	if !install("keyboard", whKeyboardLL, keyboardCallback()) {
		return
	}
	if withMouse && !install("mouse", whMouseLL, mouseCallback()) {
		return
	}

	tid := windows.GetCurrentThreadId()
	defer context.AfterFunc(ctx, func() { quitMessageLoop(tid) })()
	runMessageLoop(&h.msg, nil)
	if ctx.Err() == nil {
		slog.Warn("input hook message loop ended")
	}
}
