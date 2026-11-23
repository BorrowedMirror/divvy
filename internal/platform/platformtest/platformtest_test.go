package platformtest

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

const item = "Item Class: Stackable Currency\nRarity: Currency\nDivine Orb\n"

var (
	copyChord     = platform.CopyChord(false)
	advancedChord = platform.CopyChord(true)
)

func rect(x int32) platform.Rect {
	return platform.Rect{X: x, Y: 0, Width: 800, Height: 600}
}

// drain returns everything already in the channel, without blocking.
func drain[T any](ch <-chan T) []T {
	var out []T
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, v)
		default:
			return out
		}
	}
}

// text reads c through the platform interface; "" when it holds no text.
func text(t *testing.T, c platform.Clipboard) string {
	t.Helper()
	s, ok, err := c.Text()
	require.NoError(t, err)
	if !ok {
		return ""
	}
	return s
}

// peek reads c without recording a Get; "" when it holds no text.
func peek(c *Clipboard) string {
	s, _ := c.Peek()
	return s
}

func TestClipboardSharesStateAndRecordsHistory(t *testing.T) {
	a := NewClipboard("")
	b := a // a handle: the same pointer
	_, ok := a.Peek()
	assert.False(t, ok)
	assert.Empty(t, text(t, a))
	require.NoError(t, b.SetText("hi"))
	assert.Equal(t, "hi", text(t, a))
	require.NoError(t, a.Clear())
	_, ok = b.Peek()
	assert.False(t, ok)
	assert.Equal(t, []ClipboardOp{{Kind: OpGet}, {Kind: OpSet, Text: "hi"}, {Kind: OpGet}, {Kind: OpClear}}, a.History())
	b.Put("external")
	assert.Equal(t, "external", peek(a))
	assert.Len(t, a.History(), 4, "put/peek are not recorded")
	assert.False(t, a.HasNonText())
}

func TestClipboardFailureInjection(t *testing.T) {
	c := NewClipboard("x")
	c.SetFailing(true)
	_, _, err := c.Text()
	require.ErrorIs(t, err, platform.ErrBackend)
	require.Error(t, c.SetText("y"))
	require.Error(t, c.Clear())
	c.SetFailing(false)
	assert.Equal(t, "x", text(t, c))
}

func TestSimulatorWritesScriptedText(t *testing.T) {
	clip := NewClipboard("")
	sim := NewInputSimulator(clip)
	sim.Respond(item)
	sim.OnChord(advancedChord, "ADV")
	require.NoError(t, sim.SendChord(copyChord))
	assert.Equal(t, item, peek(clip))
	require.NoError(t, sim.SendChord(advancedChord))
	assert.Equal(t, "ADV", peek(clip))
	assert.Equal(t, []platform.Chord{copyChord, advancedChord}, sim.Sent())
	assert.Same(t, clip, sim.Clipboard())
}

func TestSimulatorWithoutScriptOnlyRecords(t *testing.T) {
	clip := NewClipboard("old")
	sim := NewInputSimulator(clip)
	require.NoError(t, sim.SendChord(copyChord))
	assert.Equal(t, "old", peek(clip))
	sim.Respond("new")
	require.NoError(t, sim.SendChord(copyChord))
	assert.Equal(t, "new", peek(clip))
	sim.Respond("")
	clip.PutNothing()
	require.NoError(t, sim.SendChord(copyChord))
	_, ok := clip.Peek()
	assert.False(t, ok)
}

func TestSimulatorDelayWritesLater(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clip := NewClipboard("")
		sim := NewInputSimulator(clip)
		sim.Respond(item)
		sim.SetDelay(30 * time.Millisecond)
		require.NoError(t, sim.SendChord(copyChord))
		assert.Empty(t, peek(clip), "not written synchronously")
		time.Sleep(30 * time.Millisecond)
		synctest.Wait()
		assert.Equal(t, item, peek(clip))
	})
}

func TestSimulatorFailure(t *testing.T) {
	sim := NewInputSimulator(NewClipboard(""))
	sim.Respond(item)
	sim.SetFailing(true)
	err := sim.SendChord(copyChord)
	require.ErrorIs(t, err, platform.ErrPermissionDenied)
	pe, ok := errors.AsType[*platform.Error](err)
	require.True(t, ok)
	assert.Equal(t, string(platform.PermissionAccessibility), pe.Detail)
	assert.Empty(t, sim.Sent())
	assert.Empty(t, peek(sim.Clipboard()))
}

func TestTrackerEmitsTransitions(t *testing.T) {
	tr := NewWindowTracker()
	_, ok := tr.GameWindow()
	assert.False(t, ok)
	assert.False(t, tr.IsGameFocused())
	rx, err := tr.Subscribe(t.Context())
	require.NoError(t, err)
	_, err = tr.Subscribe(t.Context())
	require.ErrorIs(t, err, platform.ErrAlreadyStarted)

	tr.CloseWindow()
	tr.SetWindow(GameWindow(rect(0)))
	tr.SetWindow(GameWindow(rect(10)))
	tr.SetFocused(false)
	tr.SetFocused(false)
	other := GameWindow(rect(10))
	other.NativeID = 2
	tr.SetWindow(other)
	tr.CloseWindow()
	tr.SetFocused(true) // no window: no-op

	assert.Equal(t, []platform.WindowEvent{
		{Type: platform.WindowFound, Info: GameWindow(rect(0))},
		{Type: platform.WindowMoved, Rect: rect(10)},
		{Type: platform.WindowFocusChanged, Focused: false},
		{Type: platform.WindowFound, Info: other},
		{Type: platform.WindowLost},
	}, drain(rx))
	assert.True(t, tr.Emit(platform.WindowEvent{Type: platform.WindowLost}))
	tr.Disconnect()
	assert.False(t, tr.Emit(platform.WindowEvent{Type: platform.WindowLost}))
}

func TestTrackerBuffersEventsBeforeSubscribe(t *testing.T) {
	p, h := New(Options{Window: GameWindow(rect(1))})
	assert.True(t, p.WindowTracker.IsGameFocused())
	w, ok := p.WindowTracker.GameWindow()
	require.True(t, ok)
	assert.Equal(t, platform.GameWindowTitle, w.Title)
	focused := platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: true}
	assert.True(t, h.WindowTracker.Emit(focused))
	rx, err := p.WindowTracker.Subscribe(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []platform.WindowEvent{focused}, drain(rx), "a window present from the start emits nothing")
}

func TestStreamsCloseWhenTheContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := NewWindowTracker()
		ctx, cancel := context.WithCancel(t.Context())
		rx, err := tr.Subscribe(ctx)
		require.NoError(t, err)
		l := NewInputListener()
		input, err := l.Start(ctx)
		require.NoError(t, err)

		cancel()
		synctest.Wait()
		_, open := <-rx
		assert.False(t, open, "tracker stream closed")
		_, open = <-input
		assert.False(t, open, "input stream closed")
		assert.False(t, tr.Emit(platform.WindowEvent{Type: platform.WindowLost}))
		assert.False(t, l.Emit(platform.InputEvent{Type: platform.KeyDown, Key: "Escape"}))
	})
}

func TestHotkeysRegisterFireAndConflict(t *testing.T) {
	h := NewHotkeyHost()
	d, err := platform.ParseChord("Ctrl+D")
	require.NoError(t, err)
	altD, err := platform.ParseChord("Ctrl+Alt+D")
	require.NoError(t, err)
	priceCheck := platform.Action(platform.PriceCheck)
	pinned := platform.Action(platform.PriceCheckPinned)
	settings := platform.Action(platform.OpenSettings)

	require.NoError(t, h.Register(priceCheck, d))
	// re-registering the same action with the same chord is fine
	require.NoError(t, h.Register(priceCheck, d))
	require.ErrorIs(t, h.Register(pinned, d), platform.ErrHotkeyConflict)
	h.Block(altD)
	require.ErrorIs(t, h.Register(pinned, altD), platform.ErrHotkeyConflict)
	assert.Len(t, h.Registered(), 1)

	rx, err := h.Events()
	require.NoError(t, err)
	_, err = h.Events()
	require.Error(t, err)
	assert.True(t, h.Fire(settings))
	got, ok := h.Press(d)
	assert.True(t, ok)
	assert.Equal(t, priceCheck, got)
	_, ok = h.Press(altD)
	assert.False(t, ok)
	var actions []platform.HotkeyAction
	for _, e := range drain(rx) {
		actions = append(actions, e.Action)
	}
	assert.Equal(t, []platform.HotkeyAction{settings, priceCheck}, actions)

	require.NoError(t, h.Unregister(priceCheck))
	assert.Empty(t, h.Registered())
	require.NoError(t, h.Register(priceCheck, d))
	require.NoError(t, h.Register(settings, platform.NewChord("Comma", platform.Ctrl)))
	require.NoError(t, h.UnregisterAll())
	assert.Empty(t, h.Registered())
	assert.Empty(t, h.Notes())
}

func TestInputListenerStartsOnce(t *testing.T) {
	l := NewInputListener()
	assert.False(t, l.IsStarted())
	wheel := platform.InputEvent{Type: platform.Wheel, Delta: 1}
	esc := platform.InputEvent{Type: platform.KeyDown, Key: "Escape"}
	assert.True(t, l.Emit(wheel))
	rx, err := l.Start(t.Context())
	require.NoError(t, err)
	assert.True(t, l.IsStarted())
	_, err = l.Start(t.Context())
	require.ErrorIs(t, err, platform.ErrAlreadyStarted)
	assert.True(t, l.Emit(esc))
	assert.Equal(t, []platform.InputEvent{wheel, esc}, drain(rx))
}

func TestOverlayRecordsCalls(t *testing.T) {
	o := NewOverlay()
	_, ok := o.Rect()
	assert.False(t, ok)
	require.NoError(t, o.Prepare())
	require.NoError(t, o.ShowAt(rect(5)))
	assert.True(t, o.IsVisible())
	r, ok := o.Rect()
	assert.True(t, ok)
	assert.Equal(t, rect(5), r)
	require.NoError(t, o.SetClickThrough(true))
	assert.True(t, o.IsClickThrough())
	require.NoError(t, o.Hide())
	assert.False(t, o.IsVisible())
	assert.Equal(t, []OverlayCall{
		{Kind: CallPrepare},
		{Kind: CallShowAt, Rect: rect(5)},
		{Kind: CallSetClickThrough, Enabled: true},
		{Kind: CallHide},
	}, o.Calls())
}

func TestNewWiresEverything(t *testing.T) {
	p, h := New(Options{ItemText: item, AdvancedText: "ADV", Window: GameWindow(rect(0)), Clipboard: "previous"})
	assert.Equal(t, platform.BackendFake, p.Kind)
	assert.Equal(t, Capabilities(), p.Capabilities)
	assert.Equal(t, "previous", text(t, p.Clipboard))
	require.NoError(t, p.InputSimulator.SendChord(copyChord))
	assert.Equal(t, item, peek(h.Clipboard))
	require.NoError(t, p.InputSimulator.SendChord(advancedChord))
	assert.Equal(t, "ADV", text(t, p.Clipboard))
	assert.True(t, p.WindowTracker.IsGameFocused())
	rx, err := p.InputListener.Start(t.Context())
	require.NoError(t, err)
	move := platform.InputEvent{Type: platform.MouseMove, X: 1, Y: 2}
	h.InputListener.Emit(move)
	assert.Equal(t, []platform.InputEvent{move}, drain(rx))
	assert.Len(t, h.InputSimulator.Sent(), 2)
}

func TestNewWithCapsAndDelay(t *testing.T) {
	caps := platform.Capabilities{PassiveHook: true}
	synctest.Test(t, func(t *testing.T) {
		p, h := New(Options{Caps: caps, ItemText: item, CopyDelay: time.Millisecond})
		assert.Equal(t, caps, p.Capabilities)
		require.NoError(t, p.InputSimulator.SendChord(copyChord))
		time.Sleep(time.Millisecond)
		synctest.Wait()
		assert.Equal(t, item, peek(h.Clipboard))
	})
}

func TestZeroOptions(t *testing.T) {
	p, _ := New(Options{})
	_, ok := p.WindowTracker.GameWindow()
	assert.False(t, ok)
	assert.Empty(t, text(t, p.Clipboard))
	require.NoError(t, p.InputSimulator.SendChord(copyChord))
	assert.Empty(t, text(t, p.Clipboard), "the game copies nothing")
}

func TestFakesDriveTheCopyFlow(t *testing.T) {
	p, h := New(Options{ItemText: item, Clipboard: "previous"})
	got, err := platform.CopyItemText(p.InputSimulator, p.Clipboard, platform.DefaultCopyOptions())
	require.NoError(t, err)
	assert.Equal(t, item, got)
	assert.Equal(t, "previous", peek(h.Clipboard))
}
