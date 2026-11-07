package testkit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

const fakesItem = "Item Class: Stackable Currency\nRarity: Currency\nDivine Orb\n"

func rect(x int32) platform.Rect {
	return platform.Rect{X: x, Y: 0, Width: 800, Height: 600}
}

// drain is Rust's try_iter: everything already in the channel, without blocking.
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

func text(t *testing.T, c platform.Clipboard) *string {
	t.Helper()
	s, ok, err := c.Text()
	require.NoError(t, err)
	if !ok {
		return nil
	}
	return &s
}

func peek(c *FakeClipboard) *string {
	s, ok := c.Peek()
	if !ok {
		return nil
	}
	return &s
}

//go:fix inline
func strp(s string) *string { return new(s) }

func TestClipboardSharesStateAndRecordsHistory(t *testing.T) {
	a := NewFakeClipboard()
	b := a // a handle: the same pointer
	assert.Nil(t, text(t, a))
	require.NoError(t, b.SetText("hi"))
	assert.Equal(t, new("hi"), text(t, a))
	require.NoError(t, a.Clear())
	assert.Nil(t, peek(b))
	assert.Equal(t, []ClipboardOp{{Kind: OpGet}, {Kind: OpSet, Text: "hi"}, {Kind: OpGet}, {Kind: OpClear}}, a.History())
	b.Put("external")
	assert.Equal(t, new("external"), peek(a))
	assert.Len(t, a.History(), 4, "put/peek are not recorded")
	assert.False(t, a.HasNonText())
}

func TestClipboardFailureInjection(t *testing.T) {
	c := FakeClipboardWithText("x")
	c.SetFailing(true)
	_, _, err := c.Text()
	assert.ErrorIs(t, err, platform.ErrBackend)
	assert.Error(t, c.SetText("y"))
	assert.Error(t, c.Clear())
	c.SetFailing(false)
	assert.Equal(t, new("x"), text(t, c))
}

func TestSimulatorWritesScriptedText(t *testing.T) {
	clip := NewFakeClipboard()
	sim := NewFakeInputSimulator(clip).Responding(fakesItem).OnChord(AdvancedCopyChord(), "ADV")
	require.NoError(t, sim.SendChord(CopyChord()))
	assert.Equal(t, new(fakesItem), peek(clip))
	require.NoError(t, sim.SendChord(AdvancedCopyChord()))
	assert.Equal(t, new("ADV"), peek(clip))
	assert.Equal(t, []platform.Chord{CopyChord(), AdvancedCopyChord()}, sim.Sent())
	assert.Same(t, sim.Clipboard(), sim.Clipboard())
}

func TestSimulatorWithoutScriptOnlyRecords(t *testing.T) {
	clip := FakeClipboardWithText("old")
	sim := NewFakeInputSimulator(clip)
	require.NoError(t, sim.SendChord(CopyChord()))
	assert.Equal(t, new("old"), peek(clip))
	sim.SetResponse(new("new"))
	require.NoError(t, sim.SendChord(CopyChord()))
	assert.Equal(t, new("new"), peek(clip))
	sim.SetResponse(nil)
	clip.PutNothing()
	require.NoError(t, sim.SendChord(CopyChord()))
	assert.Nil(t, peek(clip))
}

func TestSimulatorDelayWritesLater(t *testing.T) {
	clip := NewFakeClipboard()
	sim := NewFakeInputSimulator(clip).Responding(fakesItem).WithDelay(30 * time.Millisecond)
	require.NoError(t, sim.SendChord(CopyChord()))
	assert.Nil(t, peek(clip), "not written synchronously")
	deadline := time.Now().Add(5 * time.Second)
	for peek(clip) == nil {
		require.True(t, time.Now().Before(deadline), "delayed write never happened")
		time.Sleep(5 * time.Millisecond)
	}
	assert.Equal(t, new(fakesItem), peek(clip))
}

func TestSimulatorFailure(t *testing.T) {
	sim := NewFakeInputSimulator(NewFakeClipboard()).Responding(fakesItem)
	sim.SetFailing(true)
	err := sim.SendChord(CopyChord())
	var pe *platform.Error
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, platform.KindPermissionDenied, pe.Kind)
	assert.Equal(t, "accessibility", pe.Detail)
	assert.Empty(t, sim.Sent())
	assert.Nil(t, peek(sim.Clipboard()))
}

func TestTrackerEmitsTransitions(t *testing.T) {
	tr := NewFakeWindowTracker()
	_, ok := tr.GameWindow()
	assert.False(t, ok)
	assert.False(t, tr.IsGameFocused())
	rx, err := tr.Subscribe()
	require.NoError(t, err)
	_, err = tr.Subscribe()
	assert.ErrorIs(t, err, platform.ErrAlreadyStarted)

	w0, w10 := GameWindow(rect(0)), GameWindow(rect(10))
	tr.SetWindow(nil)
	tr.SetWindow(&w0)
	tr.SetWindow(&w10)
	tr.SetFocused(false)
	tr.SetFocused(false)
	other := GameWindow(rect(10))
	other.NativeID = 2
	tr.SetWindow(&other)
	tr.SetWindow(nil)
	tr.SetFocused(true) // no window: no-op

	assert.Equal(t, []platform.WindowEvent{
		{Type: platform.WindowFound, Info: GameWindow(rect(0))},
		{Type: platform.WindowMoved, Rect: rect(10)},
		{Type: platform.WindowFocusChanged, Focused: false},
		{Type: platform.WindowFound, Info: other},
		{Type: platform.WindowLost},
	}, drain(rx))
	assert.True(t, tr.Emit(platform.WindowEvent{Type: platform.WindowLost}))
	tr.Disconnect() // Rust: drop(rx)
	assert.False(t, tr.Emit(platform.WindowEvent{Type: platform.WindowLost}))
}

func TestTrackerWithWindowAndPresubscribeBuffering(t *testing.T) {
	tr := FakeWindowTrackerWithWindow(GameWindow(rect(1)))
	assert.True(t, tr.IsGameFocused())
	w, ok := tr.GameWindow()
	require.True(t, ok)
	assert.Equal(t, platform.GameWindowTitle, w.Title)
	focused := platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: true}
	assert.True(t, tr.Emit(focused))
	rx, err := tr.Subscribe()
	require.NoError(t, err)
	assert.Equal(t, []platform.WindowEvent{focused}, drain(rx))
}

func TestHotkeysRegisterFireAndConflict(t *testing.T) {
	h := NewFakeHotkeyHost()
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
	assert.ErrorIs(t, h.Register(pinned, d), platform.ErrHotkeyConflict)
	h.Block(altD)
	assert.ErrorIs(t, h.Register(pinned, altD), platform.ErrHotkeyConflict)
	assert.Len(t, h.Registered(), 1)

	rx, err := h.Events()
	require.NoError(t, err)
	_, err = h.Events()
	assert.Error(t, err)
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
	l := NewFakeInputListener()
	assert.False(t, l.IsStarted())
	wheel := platform.InputEvent{Type: platform.Wheel, Delta: 1}
	esc := platform.InputEvent{Type: platform.KeyDown, Key: "Escape"}
	assert.True(t, l.Emit(wheel))
	rx, err := l.Start()
	require.NoError(t, err)
	assert.True(t, l.IsStarted())
	_, err = l.Start()
	assert.ErrorIs(t, err, platform.ErrAlreadyStarted)
	assert.True(t, l.Emit(esc))
	assert.Equal(t, []platform.InputEvent{wheel, esc}, drain(rx))
}

func TestOverlayRecordsCalls(t *testing.T) {
	o := NewFakeOverlay()
	o2 := o
	require.NoError(t, o.Prepare())
	require.NoError(t, o.ShowAt(rect(5)))
	assert.True(t, o2.IsVisible())
	r, ok := o2.Rect()
	assert.True(t, ok)
	assert.Equal(t, rect(5), r)
	require.NoError(t, o.SetClickThrough(true))
	assert.True(t, o2.IsClickThrough())
	require.NoError(t, o.Hide())
	assert.False(t, o2.IsVisible())
	assert.Equal(t, []OverlayCall{
		{Kind: CallPrepare},
		{Kind: CallShowAt, Rect: rect(5)},
		{Kind: CallSetClickThrough, Enabled: true},
		{Kind: CallHide},
	}, o2.Calls())
}

func TestPlatformBuilderWiresEverything(t *testing.T) {
	p, h := NewFakePlatformBuilder().
		ItemText(fakesItem).
		AdvancedText("ADV").
		Window(GameWindow(rect(0))).
		Clipboard("previous").
		Build()
	assert.Equal(t, platform.BackendFake, p.Kind)
	assert.True(t, p.Capabilities.Clipboard)
	assert.Equal(t, new("previous"), text(t, p.Clipboard))
	require.NoError(t, p.InputSimulator.SendChord(CopyChord()))
	assert.Equal(t, new(fakesItem), peek(h.Clipboard))
	require.NoError(t, p.InputSimulator.SendChord(AdvancedCopyChord()))
	assert.Equal(t, new("ADV"), text(t, p.Clipboard))
	assert.True(t, p.WindowTracker.IsGameFocused())
	rx, err := p.InputListener.Start()
	require.NoError(t, err)
	move := platform.InputEvent{Type: platform.MouseMove, X: 1, Y: 2}
	h.InputListener.Emit(move)
	assert.Equal(t, []platform.InputEvent{move}, drain(rx))
	assert.Len(t, h.InputSimulator.Sent(), 2)
}

func TestPlatformBuilderCustomCapsAndDelay(t *testing.T) {
	caps := platform.Capabilities{Clipboard: false}
	p, h := NewFakePlatformBuilder().
		Capabilities(caps).
		ItemText(fakesItem).
		CopyDelay(time.Millisecond).
		Build()
	assert.Equal(t, caps, p.Capabilities)
	require.NoError(t, p.InputSimulator.SendChord(CopyChord()))
	deadline := time.Now().Add(5 * time.Second)
	for peek(h.Clipboard) == nil {
		require.True(t, time.Now().Before(deadline))
		time.Sleep(2 * time.Millisecond)
	}
}

func TestDefaultFakePlatform(t *testing.T) {
	p := FakePlatform()
	_, ok := p.WindowTracker.GameWindow()
	assert.False(t, ok)
	assert.Nil(t, text(t, p.Clipboard))
	require.NoError(t, p.InputSimulator.SendChord(CopyChord()))
	assert.Nil(t, text(t, p.Clipboard))
	assert.Len(t, FakeCapabilities().Notes, 1)
}

func TestFakesDriveTheCopyFlow(t *testing.T) {
	// Not in the Rust suite (the desktop crate covered it): the fakes plug into CopyItemText.
	p, h := NewFakePlatformBuilder().ItemText(fakesItem).Clipboard("previous").Build()
	got, err := platform.CopyItemText(p.InputSimulator, p.Clipboard, platform.DefaultCopyOptions(), func(time.Duration) {})
	require.NoError(t, err)
	assert.Equal(t, fakesItem, got)
	assert.Equal(t, new("previous"), peek(h.Clipboard))
}
