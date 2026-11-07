package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/testkit"
)

// --- ported from flow.rs ---

func TestFlowNothingCopiedStaysQuiet(t *testing.T) {
	for _, e := range []error{platform.Timeout(500 * time.Millisecond), platform.ErrNoItemText} {
		assert.True(t, IsSilentFailure(CopyError(e)), e)
	}
	backend := CopyError(platform.Backend("xdo failed"))
	assert.Equal(t, "platform", backend.Kind)
	assert.False(t, IsSilentFailure(backend))
	assert.False(t, IsSilentFailure(ipc.NewError("parse", "unknown item class")))
}

func TestFlowCloseKeys(t *testing.T) {
	assert.True(t, IsCloseKey("Escape"))
	assert.False(t, IsCloseKey("KeyD"))
}

func TestFlowCopyChords(t *testing.T) {
	assert.Equal(t, "Ctrl+C", CopyChord(false).String())
	assert.Equal(t, "Ctrl+Alt+C", CopyChord(true).String())
}

// --- behaviour through fakes ---

type fakeWindow struct {
	mu                   sync.Mutex
	visible, focused     bool
	x, y, w, h           int32
	zoom                 float64
	js                   []string
	frameless, alwaysTop bool
}

func (w *fakeWindow) Show()                { w.mu.Lock(); w.visible = true; w.mu.Unlock() }
func (w *fakeWindow) Hide()                { w.mu.Lock(); w.visible, w.focused = false, false; w.mu.Unlock() }
func (w *fakeWindow) Focus()               { w.mu.Lock(); w.focused = true; w.mu.Unlock() }
func (w *fakeWindow) IsVisible() bool      { w.mu.Lock(); defer w.mu.Unlock(); return w.visible }
func (w *fakeWindow) IsFocused() bool      { w.mu.Lock(); defer w.mu.Unlock(); return w.focused }
func (w *fakeWindow) ScaleFactor() float64 { return 1 }
func (w *fakeWindow) Bounds() (x, y, wd, h int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.x, w.y, w.w, w.h
}
func (w *fakeWindow) SetPosition(x, y int32) { w.mu.Lock(); w.x, w.y = x, y; w.mu.Unlock() }
func (w *fakeWindow) SetSize(wd, h int32)    { w.mu.Lock(); w.w, w.h = wd, h; w.mu.Unlock() }
func (w *fakeWindow) SetLogicalBounds(x, y, wd, h float64) {
	w.SetPosition(int32(x), int32(y))
	w.SetSize(int32(wd), int32(h))
}
func (w *fakeWindow) SetLogicalSize(wd, h float64) { w.SetSize(int32(wd), int32(h)) }
func (w *fakeWindow) SetZoom(z float64)            { w.mu.Lock(); w.zoom = z; w.mu.Unlock() }
func (w *fakeWindow) ExecJS(js string)             { w.mu.Lock(); w.js = append(w.js, js); w.mu.Unlock() }
func (w *fakeWindow) SetAlwaysOnTop(on bool)       { w.alwaysTop = on }
func (w *fakeWindow) SetFrameless(frameless bool)  { w.frameless = frameless }
func (w *fakeWindow) SetSkipTaskbar(bool)          {}
func (w *fakeWindow) Center()                      {}

type emitted struct {
	name    string
	payload any
}

type fakeDesktop struct {
	overlay, settings fakeWindow
	mu                sync.Mutex
	events            []emitted
	opened            []string
	tray              string
}

func (d *fakeDesktop) Overlay() Window  { return &d.overlay }
func (d *fakeDesktop) Settings() Window { return &d.settings }
func (d *fakeDesktop) CursorPosition() (x, y float64, ok bool) {
	return 500, 400, true
}
func (d *fakeDesktop) MonitorAt(float64, float64) (Area, bool) {
	return Area{X: 0, Y: 0, Width: 1920, Height: 1080}, true
}
func (d *fakeDesktop) Emit(name string, payload any) {
	d.mu.Lock()
	d.events = append(d.events, emitted{name, payload})
	d.mu.Unlock()
}
func (d *fakeDesktop) OpenURL(u string) error                   { d.opened = append(d.opened, u); return nil }
func (d *fakeDesktop) BringToFront(Window, *uint64, bool) error { return nil }
func (d *fakeDesktop) RefocusGame(*uint64)                      {}
func (d *fakeDesktop) RoundWindow(Window, int32, int32, int32)  {}
func (d *fakeDesktop) MoveDirect(Window, int32, int32) error    { return nil }
func (d *fakeDesktop) AnchorLayer(Window, bool, int32, int32) error {
	return nil
}
func (d *fakeDesktop) RelabelTray(language string) { d.tray = language }

func (d *fakeDesktop) named(name string) []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []any
	for _, e := range d.events {
		if e.name == name {
			out = append(out, e.payload)
		}
	}
	return out
}

func game() platform.WindowInfo {
	return platform.WindowInfo{Title: platform.GameWindowTitle, Rect: platform.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}, Focused: true, NativeID: 7}
}

func newTestApp(t *testing.T, b *testkit.FakePlatformBuilder) (*App, *fakeDesktop, testkit.FakeHandles) {
	t.Helper()
	p, handles := b.Build()
	st, err := NewState(nil, config.DefaultSettings(), &p, platform.BackendX11, data.Bundled)
	require.NoError(t, err)
	d := &fakeDesktop{}
	flow := NewFlow(st, d)
	flow.sleep = func(time.Duration) {}
	return &App{Flow: flow}, d, handles
}

func corpusText(t *testing.T, id string) string {
	t.Helper()
	text, err := testkit.CorpusText(id)
	require.NoError(t, err)
	return text
}

func TestFlowPriceCheckCopiesParsesAndShowsTheOverlay(t *testing.T) {
	item := corpusText(t, "gloves/rare-stocky-mitts")
	a, d, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()).ItemText(item).AdvancedText(item))
	resp, err := a.PriceCheck(context.Background(), ipc.PriceCheckRequest{Pinned: true})
	require.NoError(t, err)
	assert.Equal(t, "Stocky Mitts", resp.Item.BaseType)
	assert.True(t, d.overlay.IsVisible())
	assert.True(t, a.st.IsPinned())
	copied := d.named(ipc.EventItemCopied)
	require.Len(t, copied, 1)
	assert.True(t, copied[0].(ipc.ItemCopied).Pinned)
	snap := a.GetOverlaySnapshot()
	require.NotNil(t, snap)
	assert.Equal(t, resp.Item.BaseType, snap.Item.BaseType)
	assert.NotEmpty(t, h.InputSimulator.Sent(), "the copy chord was sent")
}

func TestFlowPriceCheckOutsideTheGameIsSilent(t *testing.T) {
	g := game()
	g.Focused = false
	a, d, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(g).ItemText("anything"))
	h.WindowTracker.SetFocused(false)
	_, _, err := a.RunPriceCheck(context.Background(), false)
	e := ipc.From(err)
	assert.Equal(t, NotFocused, e.Message)
	assert.True(t, IsSilentFailure(e))
	assert.False(t, d.overlay.IsVisible())
}

func TestFlowTriggerShowsParseFailures(t *testing.T) {
	a, d, _ := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()).ItemText("Item Class: Nonsense\nRarity: Rare\n--------\nWhat"))
	a.Trigger(false)
	require.Eventually(t, func() bool { return len(d.named(ipc.EventPriceFailed)) == 1 }, 5*time.Second, 10*time.Millisecond)
	assert.True(t, d.overlay.IsVisible())
}

func TestFlowQueryWithoutAnItemFails(t *testing.T) {
	a, _, _ := newTestApp(t, testkit.NewFakePlatformBuilder())
	_, err := a.QueryPrice(context.Background(), core.FilterSet{})
	assert.Equal(t, "no_item", ipc.From(err).Kind)
}

func TestFlowApplySettingsRollsBackHotkeysOnConflict(t *testing.T) {
	a, d, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	before := a.st.Settings()
	require.NoError(t, a.RegisterHotkeys(&before))

	next := before.Clone()
	next.Hotkeys.PriceCheck = platform.NewChord("KeyK", platform.Ctrl)
	h.Hotkeys.Block(next.Hotkeys.PriceCheck)
	_, err := a.ApplySettings(next)
	require.Error(t, err)
	assert.Equal(t, before.Hotkeys.PriceCheck, h.Hotkeys.Registered()[platform.Action(platform.PriceCheck)], "rolled back")
	assert.Empty(t, d.named(ipc.EventSettingsChanged))
	assert.Equal(t, before.Hotkeys.PriceCheck, a.st.Settings().Hotkeys.PriceCheck)
}

func TestFlowApplySettingsSavesAndBroadcasts(t *testing.T) {
	a, d, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	paths := config.Under(t.TempDir())
	a.st.Paths = &paths
	next := a.st.Settings()
	next.UI.Language = "de"
	next.Overlay.Scale = 1.25
	next.Chat.Commands = append(next.Chat.Commands, config.ChatCommand{Text: "/hideout", Send: true, Chord: platform.NewChord("F9", platform.Ctrl, platform.Shift)})
	saved, err := a.ApplySettings(next)
	require.NoError(t, err)
	assert.Equal(t, "de", saved.UI.Language)
	assert.Equal(t, "de", d.tray)
	assert.Equal(t, 1.25, d.overlay.zoom)
	require.Len(t, d.named(ipc.EventSettingsChanged), 1)
	loaded, err := config.Load(paths)
	require.NoError(t, err)
	assert.Equal(t, "de", loaded.UI.Language)
	// A new chat command's chord is registered right away.
	idx := uint8(len(next.Chat.Commands) - 1)
	assert.Equal(t, "F9", h.Hotkeys.Registered()[platform.ChatAction(idx)].Key)
}

func TestFlowHotkeysFollowGameFocus(t *testing.T) {
	a, _, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	a.st.FollowFocus.Store(true)
	h.WindowTracker.SetFocused(false)
	s := a.st.Settings()
	require.NoError(t, a.RegisterHotkeys(&s))
	assert.Empty(t, h.Hotkeys.Registered(), "released while the game is not focused")
	h.WindowTracker.SetFocused(true)
	a.SyncHotkeysToFocus()
	assert.NotEmpty(t, h.Hotkeys.Registered())
}

func TestFlowOpenTradeRefusesOtherSites(t *testing.T) {
	a, d, _ := newTestApp(t, testkit.NewFakePlatformBuilder())
	assert.Error(t, a.OpenTrade("https://example.com/trade2"))
	require.NoError(t, a.OpenTrade("https://www.pathofexile.com/trade2/search/poe2/Standard/abc"))
	assert.Equal(t, []string{"https://www.pathofexile.com/trade2/search/poe2/Standard/abc"}, d.opened)
}

func TestFlowRebindHotkeyRejectsMissingCommands(t *testing.T) {
	a, _, _ := newTestApp(t, testkit.NewFakePlatformBuilder())
	err := a.RebindHotkey(context.Background(), platform.ChatAction(40), platform.NewChord("F1"))
	assert.Equal(t, "config", ipc.From(err).Kind)
}

func TestFlowSettingsCoverTheGameWhenTracked(t *testing.T) {
	a, d, _ := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	require.NoError(t, a.OpenSettingsTab("hotkeys"))
	assert.True(t, d.settings.IsVisible())
	assert.True(t, d.settings.frameless)
	assert.Contains(t, d.settings.js[0], "dataset.overlay = '1'")
	assert.Contains(t, d.settings.js[0], "#/settings?tab=hotkeys")
	assert.Error(t, a.OpenSettingsTab("../x"))
}

func TestFlowHotkeyLoopHidesAnOpenOverlay(t *testing.T) {
	a, d, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	a.RunHotkeyLoop()
	d.overlay.Show()
	a.st.OverlayVisible.Store(true)
	h.Hotkeys.Fire(platform.Action(platform.PriceCheck))
	require.Eventually(t, func() bool { return !d.overlay.IsVisible() }, 5*time.Second, 10*time.Millisecond)
}

func TestFlowEscFromTheHookClosesAnUnpinnedPanel(t *testing.T) {
	a, d, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	a.RunInputLoop()
	d.overlay.Show()
	a.st.OverlayVisible.Store(true)
	h.InputListener.Emit(platform.InputEvent{Type: platform.KeyDown, Key: "Escape"})
	require.Eventually(t, func() bool { return !d.overlay.IsVisible() }, 5*time.Second, 10*time.Millisecond)
}

func TestFlowNothingCopiedIsNotRecordedAsAnError(t *testing.T) {
	a, _, h := newTestApp(t, testkit.NewFakePlatformBuilder().Window(game()))
	h.InputSimulator.SetResponse(nil) // the game copies nothing: the copy times out
	_, _, err := a.RunPriceCheck(context.Background(), false)
	require.Error(t, err)
	assert.True(t, IsSilentFailure(ipc.From(err)))
	assert.Empty(t, a.st.RecentErrors(), "no item under the cursor is not a diagnostics error")
}
