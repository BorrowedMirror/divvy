package mac

// The passive hook: one listen-only CGEventTap per process, on a thread of its own.

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

const (
	cgHIDEventTap          uint32 = 0  // kCGHIDEventTap
	cgHeadInsertEventTap   uint32 = 0  // kCGHeadInsertEventTap
	cgEventTapOptionListen uint32 = 1  // kCGEventTapOptionListenOnly
	fieldKeyboardKeycode   uint32 = 9  // kCGKeyboardEventKeycode
	fieldScrollPointDeltaY uint32 = 96 // kCGScrollWheelEventPointDeltaAxis1

	// hookSlice bounds how long the tap thread runs its run loop between context checks.
	hookSlice = time.Second
)

// hookStarted is true while the one process-wide hook runs (or starts).
var hookStarted atomic.Bool

// hookRun is the running tap the callback feeds. Its state is only touched on the tap thread.
type hookRun struct {
	ch    chan platform.InputEvent
	tap   uintptr
	state hookState
}

var activeHook atomic.Pointer[hookRun]

// hookCallback is the CGEventTapCallBack, created once (purego callbacks are never freed). It
// runs on the OS input path, so it only does a non-blocking send.
var hookCallback = sync.OnceValue(func() uintptr {
	return purego.NewCallback(func(proxy uintptr, typ uint32, event, userInfo uintptr) uintptr {
		run := activeHook.Load()
		if run == nil {
			return event
		}
		switch typ {
		case cgEventTapDisabledTimeout:
			slog.Error("macOS event tap disabled by timeout; re-enabling (pending key-up events may be lost)")
			cgEventTapEnable(run.tap, true)
			return event
		case cgEventTapDisabledInput:
			slog.Error("macOS event tap disabled by user input; capture through this tap has stopped")
			return event
		}
		e := cgEvent{Type: typ}
		switch typ {
		case cgEventKeyDown, cgEventKeyUp, cgEventFlagsChanged:
			e.Keycode = cgEventGetIntegerValueField(event, fieldKeyboardKeycode)
			e.Flags = cgEventGetFlags(event)
		case cgEventMouseMoved, cgEventLeftMouseDragged, cgEventRightMouseDragged, cgEventOtherMouseDragged:
			p := cgEventGetLocation(event)
			e.X, e.Y = p.X, p.Y
		case cgEventScrollWheel:
			e.WheelY = cgEventGetIntegerValueField(event, fieldScrollPointDeltaY)
		}
		if ev, ok := run.state.mapEvent(e); ok {
			platform.TrySend(run.ch, ev)
		}
		return event
	})
})

// inputListener is the passive hook. withMouse adds mouse moves and the wheel (Ctrl + wheel
// stash tab switching); otherwise only keyboard events are tapped.
type inputListener struct {
	withMouse bool
}

var _ platform.InputListener = inputListener{}

// Start implements [platform.InputListener]. It never blocks: a late start failure is logged
// and reported by the channel closing.
func (l inputListener) Start(ctx context.Context) (<-chan platform.InputEvent, error) {
	// Without Input Monitoring (or Accessibility) the tap would be created but stay silent, so
	// refuse up front and let the caller surface the missing permission.
	if !currentPermissions().canListen() {
		return nil, platform.PermissionDenied(platform.PermissionInputMonitoring)
	}
	if err := loadSys(); err != nil {
		return nil, platform.Backend("input hook: %w", err)
	}
	if !hookStarted.CompareAndSwap(false, true) {
		return nil, platform.ErrAlreadyStarted
	}
	run := &hookRun{ch: make(chan platform.InputEvent, platform.InputBufferSize)}
	go runHook(ctx, run, l.withMouse)
	return run.ch, nil
}

// runHook is the tap thread: it installs the tap on its run loop and runs it until ctx is done,
// then removes and releases the tap. The thread is never unlocked, so it exits with the
// goroutine and takes its run loop with it.
func runHook(ctx context.Context, run *hookRun, withMouse bool) {
	runtime.LockOSThread()
	defer close(run.ch) // last: no callback can run once the tap is gone
	// Free the process-wide hook before the consumer sees the channel close, so it can start a
	// new one right away.
	defer hookStarted.Store(false)

	tap := cgEventTapCreate(cgHIDEventTap, cgHeadInsertEventTap, cgEventTapOptionListen,
		hookEventMask(withMouse), hookCallback(), 0)
	if tap == 0 {
		slog.Warn("input hook failed to start: CGEventTapCreate returned null")
		return
	}
	defer cfRelease(tap)
	defer cfMachPortInvalidate(tap)
	source := cfMachPortCreateRunLoopSource(0, tap, 0)
	if source == 0 {
		slog.Warn("input hook failed to start: CFMachPortCreateRunLoopSource returned null")
		return
	}
	defer cfRelease(source)
	rl := cfRunLoopGetCurrent()
	run.tap = tap
	activeHook.Store(run)
	defer activeHook.Store(nil)
	cfRunLoopAddSource(rl, source, kCFRunLoopCommonModes)
	defer cfRunLoopRemoveSource(rl, source, kCFRunLoopCommonModes)
	cgEventTapEnable(tap, true)
	defer cgEventTapEnable(tap, false)

	release := stopRunLoopWhenDone(ctx, rl)
	defer release()
	for ctx.Err() == nil {
		if cfRunLoopRunInMode(kCFRunLoopDefaultMode, hookSlice.Seconds(), false) == cfRunLoopRunFinished {
			slog.Warn("input hook run loop has no sources; stopping")
			return
		}
	}
}
