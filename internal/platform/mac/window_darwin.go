//go:build darwin

package mac

// Game window tracking.
//
// State always comes from CGWindowListCopyWindowInfo (front-to-back on-screen list, no
// permission needed for bounds/pids; titles need Screen Recording). When Accessibility is
// granted, an AXObserver on the game's pid wakes the tracker thread's CFRunLoop on
// move/resize/focus notifications so events are immediate; otherwise the thread polls at a
// low, fixed rate. The thread only pushes into a channel.

import (
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
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

func (t *windowTracker) GameWindow() (platform.WindowInfo, bool) {
	return findGame(snapshot())
}

func (t *windowTracker) IsGameFocused() bool {
	w, ok := t.GameWindow()
	return ok && w.Focused
}

func (t *windowTracker) Subscribe() (<-chan platform.WindowEvent, error) {
	if t.subscribed.Swap(true) {
		return nil, platform.ErrAlreadyStarted
	}
	ch := make(chan platform.WindowEvent, windowChannelCapacity)
	go track(ch)
	return ch, nil
}

// track is the tracker thread body. It runs for the life of the process (a Go channel has no
// "receiver dropped" signal).
func track(ch chan<- platform.WindowEvent) {
	// The AX observer is attached to this thread's run loop.
	runtime.LockOSThread()
	var prev *platform.WindowInfo
	var watch *axWatch
	for {
		windows := snapshot()
		var pid *int32
		if raw := findGameRaw(windows); raw != nil {
			pid = &raw.PID
		}
		var next *platform.WindowInfo
		if w, ok := findGame(windows); ok {
			next = &w
		}
		for _, ev := range diff(prev, next) {
			select {
			case ch <- ev:
			default:
				slog.Warn("window event channel full; dropping event")
			}
		}
		prev = next

		// (Re)attach the AX observer when the game process changes or AX becomes available.
		if !samePID(watch, pid) {
			if watch != nil {
				watch.close()
				watch = nil
			}
			if pid != nil && accessibility() {
				watch = newAXWatch(*pid)
			}
		}

		switch {
		case watch != nil:
			watch.wait(axSlice)
		case prev != nil:
			time.Sleep(pollTracking)
		default:
			time.Sleep(pollIdle)
		}
	}
}

func samePID(w *axWatch, pid *int32) bool {
	if w == nil || pid == nil {
		return w == nil && pid == nil
	}
	return w.pid == *pid
}

// snapshot returns the on-screen windows, front to back, reduced to plain values.
func snapshot() []RawWindow {
	if loadSys() != nil {
		return nil
	}
	list := cgWindowListCopyWindowInfo(cgWindowListOptionOnScreenOnly|cgWindowListExcludeDesktopElements, cgNullWindowID)
	if list == 0 {
		return nil
	}
	defer cfRelease(list)
	n := cfArrayGetCount(list)
	out := make([]RawWindow, 0, n)
	for i := range n {
		out = append(out, rawWindow(cfArrayGetValueAtIndex(list, i)))
	}
	return out
}

func rawWindow(dict uintptr) RawWindow {
	var w RawWindow
	if dict == 0 {
		return w
	}
	get := func(key uintptr) uintptr { return cfDictionaryGetValue(dict, key) }
	if b := get(kCGWindowBounds); b != 0 && cfGetTypeID(b) == cfDictionaryGetTypeID() {
		f := func(key uintptr) float64 {
			v, _ := numberF64(cfDictionaryGetValue(b, key))
			return v
		}
		w.X, w.Y, w.Width, w.Height = f(cfStrX), f(cfStrY), f(cfStrWidth), f(cfStrHeight)
	}
	number, _ := numberI64(get(kCGWindowNumber))
	w.Number = uint32(number)
	w.PID, _ = numberI32(get(kCGWindowOwnerPID))
	w.Layer, _ = numberI64(get(kCGWindowLayer))
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

// wait blocks until an AX notification arrives or timeout passes.
func (w *axWatch) wait(timeout time.Duration) {
	if cfRunLoopRunInMode(kCFRunLoopDefaultMode, timeout.Seconds(), true) == cfRunLoopRunFinished {
		// No sources (should not happen while attached): never spin.
		time.Sleep(pollTracking)
	}
}

func (w *axWatch) close() {
	cfRunLoopRemoveSource(w.runLoop, w.source, kCFRunLoopDefaultMode)
	cfRelease(w.observer)
}
