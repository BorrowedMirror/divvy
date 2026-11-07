//go:build darwin

package mac

// Passive hook (one listen-only CGEventTap per process) and keystroke injection (CGEventPost).

import (
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

const (
	// keyGap is the gap between synthetic key events so the game samples modifier state reliably.
	keyGap = 8 * time.Millisecond

	cgHIDEventTap          uint32 = 0 // kCGHIDEventTap
	cgHeadInsertEventTap   uint32 = 0 // kCGHeadInsertEventTap
	cgEventTapOptionListen uint32 = 1 // kCGEventTapOptionListenOnly
	cgEventSourceStatePriv int32  = -1
	cgEventSourceStateHID  int32  = 1
	fieldKeyboardKeycode   uint32 = 9  // kCGKeyboardEventKeycode
	fieldEventSourceUser   uint32 = 42 // kCGEventSourceUserData
	fieldScrollPointDeltaY uint32 = 96 // kCGScrollWheelEventPointDeltaAxis1
	injectedEventMarker    int64  = 100
)

// The ONE process-wide passive hook. The tap callback only does a non-blocking send into a
// bounded channel: it runs on the OS input path and must never block.
var hook struct {
	mu      sync.Mutex
	started bool
}

// hookRun is the running tap the callback feeds. Its state is only touched on the tap thread.
type hookRun struct {
	ch    chan platform.InputEvent
	tap   uintptr
	state hookState
}

var activeHook atomic.Pointer[hookRun]

// hookCallback is the CGEventTapCallBack, created once (purego callbacks are never freed).
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
			forward(run.ch, ev)
		}
		return event
	})
})

// inputListener is the passive hook. withMouse adds mouse moves and the wheel (Ctrl + wheel
// stash tab switching); otherwise only keyboard events are tapped.
type inputListener struct {
	withMouse bool
}

// hookChannelCapacity is how many events are buffered before new ones are dropped (consumer
// stalled).
const hookChannelCapacity = 1024

func (l inputListener) Start() (<-chan platform.InputEvent, error) {
	// Without Input Monitoring (or Accessibility) the tap would be created but stay silent, so
	// refuse up front and let the caller surface the missing permission.
	if !currentPermissions().CanListen() {
		return nil, platform.PermissionDenied(PermInputMonitoring)
	}
	return startGlobalHook(l.withMouse)
}

// startGlobalHook starts the tap on its own thread. A second call while the first hook lives
// returns ErrAlreadyStarted. Never blocks: a late start failure is logged and reported by the
// returned channel closing; the hook can then be started again.
func startGlobalHook(withMouse bool) (<-chan platform.InputEvent, error) {
	hook.mu.Lock()
	defer hook.mu.Unlock()
	if hook.started {
		return nil, platform.ErrAlreadyStarted
	}
	if err := loadSys(); err != nil {
		return nil, platform.Backend("input hook: %v", err)
	}
	hook.started = true
	run := &hookRun{ch: make(chan platform.InputEvent, hookChannelCapacity)}
	callback := hookCallback()
	go func() {
		// The tap's run loop source lives on this thread's run loop for the life of the hook.
		runtime.LockOSThread()
		fail := func(msg string) {
			slog.Warn("input hook failed to start: " + msg)
			activeHook.Store(nil)
			markHookStopped()
			close(run.ch) // the callback never saw this run
		}
		tap := cgEventTapCreate(cgHIDEventTap, cgHeadInsertEventTap, cgEventTapOptionListen,
			hookEventMask(withMouse), callback, 0)
		if tap == 0 {
			fail("CGEventTapCreate returned null")
			return
		}
		source := cfMachPortCreateRunLoopSource(0, tap, 0)
		if source == 0 {
			cfRelease(tap)
			fail("CFMachPortCreateRunLoopSource returned null")
			return
		}
		run.tap = tap
		activeHook.Store(run)
		cfRunLoopAddSource(cfRunLoopGetCurrent(), source, kCFRunLoopCommonModes)
		cgEventTapEnable(tap, true)
		// Blocks for the life of the hook.
		cfRunLoopRun()
		slog.Warn("input hook run loop exited")
		activeHook.Store(nil)
		markHookStopped()
	}()
	return run.ch, nil
}

func markHookStopped() {
	hook.mu.Lock()
	hook.started = false
	hook.mu.Unlock()
}

// inputSimulator synthesises chords with CGEventPost (needs Accessibility).
type inputSimulator struct {
	copyModifier CopyModifier
}

// heldModifiers returns the modifiers physically held right now (HID system state, i.e. real
// keyboards only).
func heldModifiers() []platform.KeyModifier {
	return heldFromFlags(cgEventSourceFlagsState(cgEventSourceStateHID))
}

// SendChord sends chord to the focused app. Held chord modifiers (Cmd from Cmd+D) are left down;
// extra held ones (Shift from Cmd+Shift+D) are released first. See [platform.PlanChord].
func (s *inputSimulator) SendChord(chord platform.Chord) error {
	if !accessibility() {
		return platform.PermissionDenied(PermAccessibility)
	}
	chord = remapChord(chord, s.copyModifier)
	keycode, ok := macKeycode(chord.Key)
	if !ok {
		return platform.InvalidChord(chord.String())
	}
	source := cgEventSourceCreate(cgEventSourceStatePriv)
	if source == 0 {
		return platform.Backend("failed creating event source")
	}
	defer cfRelease(source)
	held := heldModifiers()
	// Chord modifiers the user already holds stay down, so the key event carries them too.
	var down []platform.KeyModifier
	for _, m := range chord.Modifiers {
		if slices.Contains(held, m) {
			down = append(down, m)
		}
	}
	return platform.RunChord(chord.Modifiers, held, &cgKeys{source: source, keycode: keycode, down: down})
}

// cgKeys implements [platform.ChordKeys] over CGEventPost, tracking the modifier flags each
// event carries as enigo does.
type cgKeys struct {
	source  uintptr
	keycode uint16
	down    []platform.KeyModifier
}

func (k *cgKeys) post(keycode uint16, keyDown bool) error {
	event := cgEventCreateKeyboardEvent(k.source, keycode, keyDown)
	if event == 0 {
		if keyDown {
			return platform.Backend("failed creating event to press the key")
		}
		return platform.Backend("failed creating event to release the key")
	}
	defer cfRelease(event)
	cgEventSetIntegerValueField(event, fieldEventSourceUser, injectedEventMarker)
	cgEventSetFlags(event, injectFlags(k.down))
	cgEventPost(cgHIDEventTap, event)
	return nil
}

func (k *cgKeys) ReleaseHeld(m platform.KeyModifier) error { return k.Release(m) }

func (k *cgKeys) Press(m platform.KeyModifier) error {
	if !slices.Contains(k.down, m) {
		k.down = append(k.down, m)
	}
	if err := k.post(modifierKeys[m].keycode, true); err != nil {
		return err
	}
	time.Sleep(keyGap)
	return nil
}

func (k *cgKeys) Release(m platform.KeyModifier) error {
	k.down = slices.DeleteFunc(k.down, func(d platform.KeyModifier) bool { return d == m })
	err := k.post(modifierKeys[m].keycode, false)
	if err != nil {
		slog.Warn("failed to release modifier", "error", err, "modifier", m)
	}
	return err
}

func (k *cgKeys) Click() error {
	if err := k.post(k.keycode, true); err != nil {
		return err
	}
	if err := k.post(k.keycode, false); err != nil {
		return err
	}
	time.Sleep(keyGap)
	return nil
}
