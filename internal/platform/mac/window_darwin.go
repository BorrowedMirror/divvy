package mac

// Game window tracking.
//
// State always comes from CGWindowListCopyWindowInfo (front-to-back on-screen list, no
// permission needed for bounds/pids; titles need Screen Recording). When Accessibility is
// granted, an AXObserver on the game's pid wakes the tracker thread's CFRunLoop on
// move/resize/focus notifications so events are immediate; otherwise the thread polls at a
// low, fixed rate. The thread only pushes into a channel.

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

const (
	// pollTracking is the poll interval while the game is visible but no AX observer is attached.
	pollTracking = 250 * time.Millisecond
	// pollIdle is the poll interval while the game is not running / not on screen.
	pollIdle = time.Second
	// axSlice is the safety-net re-snapshot interval while an AX observer is attached.
	axSlice = time.Second
	// windowChannelCapacity bounds the window event channel.
	windowChannelCapacity = 256
)

// CGWindowListOption bits.
const (
	cgWindowListOptionOnScreenOnly     uint32 = 1 << 0
	cgWindowListExcludeDesktopElements uint32 = 1 << 4
	cgNullWindowID                     uint32 = 0
	axErrorSuccess                     int32  = 0
)

// axNotifications are the AX notifications that can change the game window's rect, focus or
// existence.
var axNotifications = []string{
	"AXWindowMoved",
	"AXWindowResized",
	"AXFocusedWindowChanged",
	"AXMainWindowChanged",
	"AXApplicationActivated",
	"AXApplicationDeactivated",
	"AXApplicationHidden",
	"AXApplicationShown",
	"AXWindowMiniaturized",
	"AXWindowDeminiaturized",
	"AXUIElementDestroyed",
}

// axNotificationStrings are the notification names as CFStrings, created once and kept.
var axNotificationStrings = sync.OnceValue(func() []uintptr {
	out := make([]uintptr, 0, len(axNotifications))
	for _, n := range axNotifications {
		out = append(out, cfString(n))
	}
	return out
})

// axCallback only exists to wake CFRunLoopRunInMode; the loop re-snapshots afterwards. purego
// callbacks are never freed, so it is created once.
var axCallback = sync.OnceValue(func() uintptr {
	return purego.NewCallback(func(observer, element, notification, refcon uintptr) {})
})

type windowTracker struct {
	subscribed atomic.Bool
}

var _ platform.WindowTracker = (*windowTracker)(nil)

func (t *windowTracker) GameWindow() (platform.WindowInfo, bool) {
	return findGame(snapshot())
}

func (t *windowTracker) IsGameFocused() bool {
	w, ok := t.GameWindow()
	return ok && w.Focused
}

// Subscribe implements [platform.WindowTracker]. The tracker thread runs until ctx is done.
func (t *windowTracker) Subscribe(ctx context.Context) (<-chan platform.WindowEvent, error) {
	if !t.subscribed.CompareAndSwap(false, true) {
		return nil, platform.ErrAlreadyStarted
	}
	ch := make(chan platform.WindowEvent, windowChannelCapacity)
	go track(ctx, ch)
	return ch, nil
}

// track is the tracker thread body. The thread is never unlocked, so it exits with the
// goroutine and takes its run loop (and any AX observer source left on it) with it.
func track(ctx context.Context, ch chan<- platform.WindowEvent) {
	// The AX observer is attached to this thread's run loop.
	runtime.LockOSThread()
	defer close(ch)
	release := stopRunLoopWhenDone(ctx, cfRunLoopGetCurrent())
	defer release()
	var prev *platform.WindowInfo
	var watch *axWatch
	defer func() {
		if watch != nil {
			watch.close()
		}
	}()
	for ctx.Err() == nil {
		windows := snapshot()
		var next *platform.WindowInfo
		if w, ok := findGame(windows); ok {
			next = &w
		}
		for _, ev := range platform.DiffWindows(prev, next) {
			if !platform.TrySend(ch, ev) {
				slog.Warn("window event channel full; dropping event")
			}
		}
		prev = next

		// (Re)attach the AX observer when the game process changes or AX becomes available.
		raw := findGameRaw(windows)
		if !watch.watches(raw) {
			if watch != nil {
				watch.close()
				watch = nil
			}
			if raw != nil && accessibility() {
				watch = newAXWatch(raw.PID)
			}
		}

		switch {
		case watch != nil:
			watch.wait(ctx, axSlice)
		case prev != nil:
			sleep(ctx, pollTracking)
		default:
			sleep(ctx, pollIdle)
		}
	}
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// snapshot returns the on-screen windows, front to back, reduced to plain values.
func snapshot() []rawWindow {
	if loadSys() != nil {
		return nil
	}
	list := cgWindowListCopyWindowInfo(cgWindowListOptionOnScreenOnly|cgWindowListExcludeDesktopElements, cgNullWindowID)
	if list == 0 {
		return nil
	}
	defer cfRelease(list)
	n := cfArrayGetCount(list)
	out := make([]rawWindow, 0, n)
	for i := range n {
		out = append(out, windowFromDict(cfArrayGetValueAtIndex(list, i)))
	}
	return out
}

// windowFromDict reads one CGWindowList entry.
func windowFromDict(dict uintptr) rawWindow {
	var w rawWindow
	if dict == 0 {
		return w
	}
	get := func(key uintptr) uintptr { return cfDictionaryGetValue(dict, key) }
	if b := get(kCGWindowBounds); b != 0 && cfGetTypeID(b) == cfDictionaryGetTypeID() {
		f := func(key uintptr) float64 {
			return numberF64(cfDictionaryGetValue(b, key))
		}
		w.X, w.Y, w.Width, w.Height = f(cfStrX), f(cfStrY), f(cfStrWidth), f(cfStrHeight)
	}
	w.Number = uint32(numberI64(get(kCGWindowNumber)))
	w.PID = numberI32(get(kCGWindowOwnerPID))
	w.Layer = numberI64(get(kCGWindowLayer))
	if s, ok := goString(get(kCGWindowOwnerName)); ok {
		w.OwnerName = &s
	}
	if s, ok := goString(get(kCGWindowName)); ok {
		w.Title = &s
	}
	return w
}

// axWatch is an AXObserver for one pid, attached to the current thread's run loop.
type axWatch struct {
	pid      int32
	runLoop  uintptr
	source   uintptr
	observer uintptr // owned (+1), kept for as long as the source is scheduled
}

func newAXWatch(pid int32) *axWatch {
	var observer uintptr
	if err := axObserverCreate(pid, axCallback(), &observer); err != axErrorSuccess || observer == 0 {
		slog.Debug("AXObserverCreate failed; falling back to polling", "error", err, "pid", pid)
		return nil
	}
	// Creating an application element for any pid is valid; it may simply refer to a process
	// that has exited, in which case notifications fail to register below.
	app := axUIElementCreateApplication(pid)
	if app != 0 {
		defer cfRelease(app)
	}
	registered := 0
	for _, name := range axNotificationStrings() {
		if axObserverAddNotification(observer, app, name, 0) == axErrorSuccess {
			registered++
		}
	}
	if registered == 0 {
		slog.Debug("no AX notifications registered; falling back to polling", "pid", pid)
		cfRelease(observer)
		return nil
	}
	// Get rule: the observer owns the source.
	source := axObserverGetRunLoopSource(observer)
	runLoop := cfRunLoopGetCurrent()
	if source == 0 || runLoop == 0 {
		cfRelease(observer)
		return nil
	}
	cfRunLoopAddSource(runLoop, source, kCFRunLoopDefaultMode)
	return &axWatch{pid: pid, runLoop: runLoop, source: source, observer: observer}
}

// watches reports whether w observes the process owning raw (both nil counts as a match).
func (w *axWatch) watches(raw *rawWindow) bool {
	if w == nil || raw == nil {
		return w == nil && raw == nil
	}
	return w.pid == raw.PID
}

// wait blocks until an AX notification arrives, timeout passes or ctx is done (which stops the
// run loop).
func (w *axWatch) wait(ctx context.Context, timeout time.Duration) {
	if cfRunLoopRunInMode(kCFRunLoopDefaultMode, timeout.Seconds(), true) == cfRunLoopRunFinished {
		// No sources (should not happen while attached): never spin.
		sleep(ctx, pollTracking)
	}
}

func (w *axWatch) close() {
	cfRunLoopRemoveSource(w.runLoop, w.source, kCFRunLoopDefaultMode)
	cfRelease(w.observer)
}
