package platformtest

import (
	"context"
	"sync"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// GameWindow is a plausible focused game window at rect.
func GameWindow(rect platform.Rect) platform.WindowInfo {
	return platform.WindowInfo{Title: platform.GameWindowTitle, Rect: rect, Focused: true, NativeID: 0x00c0_ffee}
}

// WindowTracker is a window tracker whose game window is set by the test.
type WindowTracker struct {
	mu     sync.Mutex
	window *platform.WindowInfo // nil: no game window
	events stream[platform.WindowEvent]
}

var _ platform.WindowTracker = (*WindowTracker)(nil)

// NewWindowTracker returns a tracker that sees no game window.
func NewWindowTracker() *WindowTracker { return &WindowTracker{} }

// SetWindow makes window the game window and emits the matching events
// ([platform.DiffWindows]).
func (t *WindowTracker) SetWindow(window platform.WindowInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.replace(&window)
}

// CloseWindow makes the game window go away (emitting WindowLost if there was one).
func (t *WindowTracker) CloseWindow() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.replace(nil)
}

func (t *WindowTracker) replace(window *platform.WindowInfo) {
	for _, ev := range platform.DiffWindows(t.window, window) {
		t.events.emit(ev)
	}
	t.window = window
}

// SetFocused changes only the focus flag (emits FocusChanged when it changes). Without a window
// it does nothing.
func (t *WindowTracker) SetFocused(focused bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.window == nil {
		return
	}
	w := *t.window
	w.Focused = focused
	t.replace(&w)
}

// Emit sends a raw event without touching state and reports whether a receiver got it.
func (t *WindowTracker) Emit(event platform.WindowEvent) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events.emit(event)
}

// Disconnect closes the event channel, as if the subscriber went away; later emits report
// false.
func (t *WindowTracker) Disconnect() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events.close()
}

// GameWindow implements [platform.WindowTracker].
func (t *WindowTracker) GameWindow() (platform.WindowInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.window == nil {
		return platform.WindowInfo{}, false
	}
	return *t.window, true
}

// IsGameFocused implements [platform.WindowTracker].
func (t *WindowTracker) IsGameFocused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.window != nil && t.window.Focused
}

// Subscribe implements [platform.WindowTracker]. The channel is closed when ctx ends.
func (t *WindowTracker) Subscribe(ctx context.Context) (<-chan platform.WindowEvent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events.subscribe(ctx, &t.mu)
}
