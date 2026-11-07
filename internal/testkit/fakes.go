// Package testkit is shared test support.
//
//   - Fakes (fakes.go): deterministic in-memory implementations of every internal/platform
//     interface.
//   - Corpus (corpus.go): the item-text corpus (internal/testkit/testdata/corpus/).
//   - Fixtures (fixtures.go): an httptest mock server serving recorded trade API responses.
//
// Every fake is a pointer handle over shared, mutex-guarded state: put it into a
// [platform.Platform] and keep the same pointer in the test to drive it (Fire, Emit, SetWindow)
// or inspect it (Calls, Sent, History).
//
//	p, fakes := testkit.NewFakePlatformBuilder().ItemText("Item Class: Stackable Currency\n").Build()
//	events, _ := p.Hotkeys.Events()
//	fakes.Hotkeys.Fire(platform.Action(platform.PriceCheck))
//	ev := <-events // ev.Action == PriceCheck
package testkit

import (
	"maps"
	"sync"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// streamBuffer is how many undelivered events a fake stream holds. Rust's mpsc channels are
// unbounded; a Go channel needs a size, and an emit into a full buffer reports false.
const streamBuffer = 1024

// stream is a one-shot event stream: subscribe may be called once, later calls get
// ErrAlreadyStarted. Events emitted before subscription are buffered and delivered on subscribe.
// The owner's mutex guards it.
type stream[T any] struct {
	ch           chan T
	started      bool
	disconnected bool
	pending      []T
}

func (s *stream[T]) subscribe() (<-chan T, error) {
	if s.started {
		return nil, &platform.Error{Kind: platform.KindAlreadyStarted}
	}
	s.started = true
	s.ch = make(chan T, max(streamBuffer, len(s.pending)))
	for _, ev := range s.pending {
		s.ch <- ev
	}
	s.pending = nil
	return s.ch, nil
}

// emit reports whether the event reached a live receiver (or was buffered pre-subscribe).
func (s *stream[T]) emit(ev T) bool {
	switch {
	case s.ch != nil && !s.disconnected:
		select {
		case s.ch <- ev:
			return true
		default:
			return false
		}
	case !s.started:
		s.pending = append(s.pending, ev)
		return true
	}
	return false
}

// disconnect closes the channel, standing in for Rust dropping the receiver: later emits
// report false.
func (s *stream[T]) disconnect() {
	if s.ch != nil && !s.disconnected {
		s.disconnected = true
		close(s.ch)
	}
}

// ---------------------------------------------------------------------------------------------
// Clipboard
// ---------------------------------------------------------------------------------------------

// ClipboardOpKind is the kind of a [ClipboardOp].
type ClipboardOpKind int

const (
	OpGet ClipboardOpKind = iota + 1
	OpSet
	OpClear
)

// ClipboardOp is one recorded clipboard operation; Text is set for OpSet.
type ClipboardOp struct {
	Kind ClipboardOpKind
	Text string
}

// FakeClipboard is an in-memory clipboard.
type FakeClipboard struct {
	mu      sync.Mutex
	text    string
	hasText bool
	history []ClipboardOp
	fail    bool
}

var _ platform.Clipboard = (*FakeClipboard)(nil)

// NewFakeClipboard returns an empty clipboard.
func NewFakeClipboard() *FakeClipboard { return &FakeClipboard{} }

// FakeClipboardWithText returns a clipboard pre-filled with text.
func FakeClipboardWithText(text string) *FakeClipboard {
	return &FakeClipboard{text: text, hasText: true}
}

// Peek returns the current contents without recording a Get.
func (c *FakeClipboard) Peek() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text, c.hasText
}

// Put overwrites the contents without recording a Set (as if another app copied).
func (c *FakeClipboard) Put(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.text, c.hasText = text, true
}

// PutNothing empties the clipboard without recording a Clear (as if another app cleared it).
func (c *FakeClipboard) PutNothing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.text, c.hasText = "", false
}

// History is every operation performed through the [platform.Clipboard] interface, in order.
func (c *FakeClipboard) History() []ClipboardOp {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ClipboardOp(nil), c.history...)
}

// SetFailing makes every interface call fail with a backend error until reset.
func (c *FakeClipboard) SetFailing(fail bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail = fail
}

func (c *FakeClipboard) check() error {
	if c.fail {
		return platform.Backend("fake clipboard failure")
	}
	return nil
}

// Text implements [platform.Clipboard].
func (c *FakeClipboard) Text() (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(); err != nil {
		return "", false, err
	}
	c.history = append(c.history, ClipboardOp{Kind: OpGet})
	return c.text, c.hasText, nil
}

// SetText implements [platform.Clipboard].
func (c *FakeClipboard) SetText(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(); err != nil {
		return err
	}
	c.history = append(c.history, ClipboardOp{Kind: OpSet, Text: text})
	c.text, c.hasText = text, true
	return nil
}

// Clear implements [platform.Clipboard].
func (c *FakeClipboard) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(); err != nil {
		return err
	}
	c.history = append(c.history, ClipboardOp{Kind: OpClear})
	c.text, c.hasText = "", false
	return nil
}

// HasNonText implements [platform.Clipboard]; the fake only holds text.
func (c *FakeClipboard) HasNonText() bool { return false }

// ---------------------------------------------------------------------------------------------
// Input simulator
// ---------------------------------------------------------------------------------------------

type chordScript struct {
	chord platform.Chord
	text  string
}

// FakeInputSimulator records chords and, like the game would, writes scripted text into a
// [FakeClipboard] in response. With a non-zero delay (WithDelay) the write happens on a
// goroutine, which exercises the polling loop of [platform.CopyItemText].
type FakeInputSimulator struct {
	clipboard *FakeClipboard

	mu sync.Mutex
	// scripts is the text per exact chord; it falls back to defaultText.
	scripts     []chordScript
	defaultText *string
	delay       time.Duration
	sent        []platform.Chord
	fail        bool
}

var _ platform.InputSimulator = (*FakeInputSimulator)(nil)

// NewFakeInputSimulator returns a simulator that writes into clipboard. With no script it only
// records chords.
func NewFakeInputSimulator(clipboard *FakeClipboard) *FakeInputSimulator {
	return &FakeInputSimulator{clipboard: clipboard}
}

// Responding makes it respond to any chord with text.
func (s *FakeInputSimulator) Responding(text string) *FakeInputSimulator {
	s.SetResponse(&text)
	return s
}

// OnChord makes it respond to exactly chord with text (takes precedence over Responding).
func (s *FakeInputSimulator) OnChord(chord platform.Chord, text string) *FakeInputSimulator {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.scripts {
		if s.scripts[i].chord.Equal(chord) {
			s.scripts[i].text = text
			return s
		}
	}
	s.scripts = append(s.scripts, chordScript{chord, text})
	return s
}

// WithDelay sets the delay before the clipboard is written.
func (s *FakeInputSimulator) WithDelay(delay time.Duration) *FakeInputSimulator {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = delay
	return s
}

// SetResponse changes the default response at runtime (nil = the game copies nothing).
func (s *FakeInputSimulator) SetResponse(text *string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if text == nil {
		s.defaultText = nil
		return
	}
	t := *text
	s.defaultText = &t
}

// SetFailing makes SendChord fail with PermissionDenied("accessibility").
func (s *FakeInputSimulator) SetFailing(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
}

// Sent returns the chords sent so far.
func (s *FakeInputSimulator) Sent() []platform.Chord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]platform.Chord(nil), s.sent...)
}

// Clipboard is the clipboard this simulator writes to.
func (s *FakeInputSimulator) Clipboard() *FakeClipboard { return s.clipboard }

// SendChord implements [platform.InputSimulator].
func (s *FakeInputSimulator) SendChord(chord platform.Chord) error {
	s.mu.Lock()
	if s.fail {
		s.mu.Unlock()
		return platform.PermissionDenied("accessibility")
	}
	s.sent = append(s.sent, chord)
	var text *string
	for _, sc := range s.scripts {
		if sc.chord.Equal(chord) {
			t := sc.text
			text = &t
			break
		}
	}
	if text == nil {
		text = s.defaultText
	}
	delay := s.delay
	s.mu.Unlock()

	if text == nil {
		return nil
	}
	t := *text
	if delay == 0 {
		s.clipboard.Put(t)
	} else {
		go func() {
			time.Sleep(delay)
			s.clipboard.Put(t)
		}()
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Window tracker
// ---------------------------------------------------------------------------------------------

// FakeWindowTracker is a window tracker whose game window is set by the test.
type FakeWindowTracker struct {
	mu     sync.Mutex
	window *platform.WindowInfo
	events stream[platform.WindowEvent]
}

var _ platform.WindowTracker = (*FakeWindowTracker)(nil)

// GameWindow is a plausible focused game window at rect.
func GameWindow(rect platform.Rect) platform.WindowInfo {
	return platform.WindowInfo{Title: platform.GameWindowTitle, Rect: rect, Focused: true, NativeID: 0x00c0_ffee}
}

// NewFakeWindowTracker returns a tracker that sees no game window.
func NewFakeWindowTracker() *FakeWindowTracker { return &FakeWindowTracker{} }

// FakeWindowTrackerWithWindow returns a tracker that already sees window.
func FakeWindowTrackerWithWindow(window platform.WindowInfo) *FakeWindowTracker {
	return &FakeWindowTracker{window: &window}
}

// SetWindow replaces the window (nil = gone) and emits the matching event (Found, Moved,
// FocusChanged, Lost).
func (t *FakeWindowTracker) SetWindow(window *platform.WindowInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.setWindowLocked(window)
}

func (t *FakeWindowTracker) setWindowLocked(window *platform.WindowInfo) {
	prev := t.window
	var events []platform.WindowEvent
	switch {
	case prev == nil && window == nil:
	case window == nil:
		events = append(events, platform.WindowEvent{Type: platform.WindowLost})
	case prev == nil, prev.NativeID != window.NativeID || prev.Title != window.Title:
		events = append(events, platform.WindowEvent{Type: platform.WindowFound, Info: *window})
	default:
		if prev.Rect != window.Rect {
			events = append(events, platform.WindowEvent{Type: platform.WindowMoved, Rect: window.Rect})
		}
		if prev.Focused != window.Focused {
			events = append(events, platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: window.Focused})
		}
	}
	if window != nil {
		w := *window
		t.window = &w
	} else {
		t.window = nil
	}
	for _, ev := range events {
		t.events.emit(ev)
	}
}

// SetFocused changes only the focus flag (emits FocusChanged when it changes). Without a window
// it does nothing.
func (t *FakeWindowTracker) SetFocused(focused bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.window == nil {
		return
	}
	w := *t.window
	w.Focused = focused
	t.setWindowLocked(&w)
}

// Emit sends a raw event without touching state and reports whether a receiver got it.
func (t *FakeWindowTracker) Emit(event platform.WindowEvent) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events.emit(event)
}

// Disconnect closes the event channel, as if the subscriber went away; later emits report
// false.
func (t *FakeWindowTracker) Disconnect() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events.disconnect()
}

// GameWindow implements [platform.WindowTracker].
func (t *FakeWindowTracker) GameWindow() (platform.WindowInfo, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.window == nil {
		return platform.WindowInfo{}, false
	}
	return *t.window, true
}

// IsGameFocused implements [platform.WindowTracker].
func (t *FakeWindowTracker) IsGameFocused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.window != nil && t.window.Focused
}

// Subscribe implements [platform.WindowTracker].
func (t *FakeWindowTracker) Subscribe() (<-chan platform.WindowEvent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events.subscribe()
}

// ---------------------------------------------------------------------------------------------
// Hotkeys
// ---------------------------------------------------------------------------------------------

// FakeHotkeyHost is a hotkey host; the test presses a hotkey with Fire.
type FakeHotkeyHost struct {
	mu         sync.Mutex
	registered map[platform.HotkeyAction]platform.Chord
	// taken are chords "owned by another application": registering them fails.
	taken  []platform.Chord
	events stream[platform.HotkeyEvent]
}

var _ platform.HotkeyHost = (*FakeHotkeyHost)(nil)

// NewFakeHotkeyHost returns a host with nothing registered.
func NewFakeHotkeyHost() *FakeHotkeyHost {
	return &FakeHotkeyHost{registered: map[platform.HotkeyAction]platform.Chord{}}
}

// Fire simulates the user pressing the hotkey for action. It fires even if action is not
// registered (tests of the consumer usually don't care) and reports whether it was delivered.
func (h *FakeHotkeyHost) Fire(action platform.HotkeyAction) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events.emit(platform.HotkeyEvent{Action: action})
}

// Press simulates pressing chord: it fires the action bound to it, if any.
func (h *FakeHotkeyHost) Press(chord platform.Chord) (platform.HotkeyAction, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for a, c := range h.registered {
		if c.Equal(chord) {
			h.events.emit(platform.HotkeyEvent{Action: a})
			return a, true
		}
	}
	return platform.HotkeyAction{}, false
}

// Block marks chord as grabbed by another application.
func (h *FakeHotkeyHost) Block(chord platform.Chord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taken = append(h.taken, chord)
}

// Registered returns a copy of the current registrations.
func (h *FakeHotkeyHost) Registered() map[platform.HotkeyAction]platform.Chord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[platform.HotkeyAction]platform.Chord, len(h.registered))
	maps.Copy(out, h.registered)
	return out
}

// Register implements [platform.HotkeyHost].
func (h *FakeHotkeyHost) Register(action platform.HotkeyAction, chord platform.Chord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.taken {
		if c.Equal(chord) {
			return platform.HotkeyConflict(chord.String())
		}
	}
	for a, c := range h.registered {
		if a != action && c.Equal(chord) {
			return platform.HotkeyConflict(chord.String())
		}
	}
	h.registered[action] = chord
	return nil
}

// RegisterAll implements [platform.HotkeyHost] one binding at a time.
func (h *FakeHotkeyHost) RegisterAll(bindings []platform.Binding) error {
	return platform.RegisterEach(h, bindings)
}

// Unregister implements [platform.HotkeyHost].
func (h *FakeHotkeyHost) Unregister(action platform.HotkeyAction) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.registered, action)
	return nil
}

// UnregisterAll implements [platform.HotkeyHost].
func (h *FakeHotkeyHost) UnregisterAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.registered)
	return nil
}

// Events implements [platform.HotkeyHost].
func (h *FakeHotkeyHost) Events() (<-chan platform.HotkeyEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events.subscribe()
}

// Notes implements [platform.HotkeyHost]; the fake never has any.
func (h *FakeHotkeyHost) Notes() []string { return nil }

// ---------------------------------------------------------------------------------------------
// Input listener
// ---------------------------------------------------------------------------------------------

// FakeInputListener is the passive hook; the test injects events with Emit.
type FakeInputListener struct {
	mu     sync.Mutex
	events stream[platform.InputEvent]
}

var _ platform.InputListener = (*FakeInputListener)(nil)

// NewFakeInputListener returns a listener that has not started.
func NewFakeInputListener() *FakeInputListener { return &FakeInputListener{} }

// Emit injects an input event and reports whether it was delivered or buffered.
func (l *FakeInputListener) Emit(event platform.InputEvent) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.events.emit(event)
}

// IsStarted reports whether Start was called.
func (l *FakeInputListener) IsStarted() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.events.started
}

// Start implements [platform.InputListener].
func (l *FakeInputListener) Start() (<-chan platform.InputEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.events.subscribe()
}

// ---------------------------------------------------------------------------------------------
// Overlay
// ---------------------------------------------------------------------------------------------

// OverlayCallKind is the kind of an [OverlayCall].
type OverlayCallKind int

const (
	CallPrepare OverlayCallKind = iota + 1
	CallShowAt
	CallHide
	CallSetClickThrough
)

// OverlayCall is one recorded [platform.OverlayWindow] call. Rect is set for CallShowAt,
// Enabled for CallSetClickThrough.
type OverlayCall struct {
	Kind    OverlayCallKind
	Rect    platform.Rect
	Enabled bool
}

// FakeOverlay is an overlay styler that records every call.
type FakeOverlay struct {
	mu           sync.Mutex
	calls        []OverlayCall
	visible      bool
	rect         *platform.Rect
	clickThrough bool
}

var _ platform.OverlayWindow = (*FakeOverlay)(nil)

// NewFakeOverlay returns a hidden overlay.
func NewFakeOverlay() *FakeOverlay { return &FakeOverlay{} }

// Calls returns the recorded calls.
func (o *FakeOverlay) Calls() []OverlayCall {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]OverlayCall(nil), o.calls...)
}

// IsVisible reports whether the overlay is shown.
func (o *FakeOverlay) IsVisible() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.visible
}

// Rect is the rect of the last ShowAt.
func (o *FakeOverlay) Rect() (platform.Rect, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.rect == nil {
		return platform.Rect{}, false
	}
	return *o.rect, true
}

// IsClickThrough reports the last SetClickThrough value.
func (o *FakeOverlay) IsClickThrough() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.clickThrough
}

// Prepare implements [platform.OverlayWindow].
func (o *FakeOverlay) Prepare() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallPrepare})
	return nil
}

// ShowAt implements [platform.OverlayWindow].
func (o *FakeOverlay) ShowAt(rect platform.Rect) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallShowAt, Rect: rect})
	o.visible = true
	o.rect = &rect
	return nil
}

// Hide implements [platform.OverlayWindow].
func (o *FakeOverlay) Hide() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallHide})
	o.visible = false
	return nil
}

// SetClickThrough implements [platform.OverlayWindow].
func (o *FakeOverlay) SetClickThrough(enabled bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallSetClickThrough, Enabled: enabled})
	o.clickThrough = enabled
	return nil
}

// RaiseAbove implements [platform.OverlayWindow] as a no-op (the Rust trait default).
func (o *FakeOverlay) RaiseAbove(*uint64) error { return nil }

// Activate implements [platform.OverlayWindow] as a no-op (the Rust trait default).
func (o *FakeOverlay) Activate() error { return nil }

// ---------------------------------------------------------------------------------------------
// Platform
// ---------------------------------------------------------------------------------------------

// FakeCapabilities are the capabilities the fake backend reports: everything works.
func FakeCapabilities() platform.Capabilities {
	return platform.Capabilities{
		WindowTracking:      true,
		OverlayAlwaysOnTop:  true,
		OverlayClickThrough: true,
		GlobalHotkeys:       true,
		PassiveHook:         true,
		KeystrokeInjection:  true,
		Clipboard:           true,
		MissingPermissions:  []string{},
		Notes:               []string{"fake backend (rkl-testkit)"},
	}
}

// FakeHandles are the test-side handles to the fakes inside a [platform.Platform] built by
// [FakePlatformBuilder]. They are the same pointers the platform holds.
type FakeHandles struct {
	Clipboard      *FakeClipboard
	InputSimulator *FakeInputSimulator
	WindowTracker  *FakeWindowTracker
	Hotkeys        *FakeHotkeyHost
	InputListener  *FakeInputListener
}

// FakePlatformBuilder builds a [platform.Platform] of fakes plus the [FakeHandles] to drive
// them.
type FakePlatformBuilder struct {
	itemText     *string
	advancedText *string
	delay        time.Duration
	window       *platform.WindowInfo
	clipboard    *string
	capabilities *platform.Capabilities
}

// NewFakePlatformBuilder returns a builder with defaults: empty clipboard, no game window,
// nothing scripted.
func NewFakePlatformBuilder() *FakePlatformBuilder { return &FakePlatformBuilder{} }

// ItemText is the text the "game" copies on any chord.
func (b *FakePlatformBuilder) ItemText(text string) *FakePlatformBuilder {
	b.itemText = &text
	return b
}

// AdvancedText is the text the "game" copies on Ctrl+Alt+C (advanced copy).
func (b *FakePlatformBuilder) AdvancedText(text string) *FakePlatformBuilder {
	b.advancedText = &text
	return b
}

// CopyDelay is the delay between the chord and the clipboard write.
func (b *FakePlatformBuilder) CopyDelay(delay time.Duration) *FakePlatformBuilder {
	b.delay = delay
	return b
}

// Window is the game window the tracker reports.
func (b *FakePlatformBuilder) Window(window platform.WindowInfo) *FakePlatformBuilder {
	b.window = &window
	return b
}

// Clipboard is the initial clipboard contents (e.g. to test restore).
func (b *FakePlatformBuilder) Clipboard(text string) *FakePlatformBuilder {
	b.clipboard = &text
	return b
}

// Capabilities overrides [FakeCapabilities].
func (b *FakePlatformBuilder) Capabilities(caps platform.Capabilities) *FakePlatformBuilder {
	b.capabilities = &caps
	return b
}

// Build creates the platform and its handles.
func (b *FakePlatformBuilder) Build() (platform.Platform, FakeHandles) {
	clipboard := NewFakeClipboard()
	if b.clipboard != nil {
		clipboard = FakeClipboardWithText(*b.clipboard)
	}
	sim := NewFakeInputSimulator(clipboard).WithDelay(b.delay)
	if b.itemText != nil {
		sim.Responding(*b.itemText)
	}
	if b.advancedText != nil {
		sim.OnChord(AdvancedCopyChord(), *b.advancedText)
	}
	tracker := NewFakeWindowTracker()
	if b.window != nil {
		tracker = FakeWindowTrackerWithWindow(*b.window)
	}
	handles := FakeHandles{
		Clipboard:      clipboard,
		InputSimulator: sim,
		WindowTracker:  tracker,
		Hotkeys:        NewFakeHotkeyHost(),
		InputListener:  NewFakeInputListener(),
	}
	caps := FakeCapabilities()
	if b.capabilities != nil {
		caps = *b.capabilities
	}
	p := platform.Platform{
		Kind:           platform.BackendFake,
		Capabilities:   caps,
		WindowTracker:  handles.WindowTracker,
		Hotkeys:        handles.Hotkeys,
		InputListener:  handles.InputListener,
		InputSimulator: handles.InputSimulator,
		Clipboard:      handles.Clipboard,
	}
	return p, handles
}

// CopyChord is `Ctrl+C`.
func CopyChord() platform.Chord { return platform.CopyChord(false) }

// AdvancedCopyChord is `Ctrl+Alt+C`.
func AdvancedCopyChord() platform.Chord { return platform.CopyChord(true) }

// FakePlatform is a fake [platform.Platform] with defaults: empty clipboard, no game window,
// nothing scripted. Use [FakePlatformBuilder] to keep handles for driving it.
func FakePlatform() platform.Platform {
	p, _ := NewFakePlatformBuilder().Build()
	return p
}
