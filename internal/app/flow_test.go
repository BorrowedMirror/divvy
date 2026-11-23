package app

import (
	"os"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/core/coretest"
	"github.com/BorrowedMirror/divvy/internal/data"
	"github.com/BorrowedMirror/divvy/internal/ipc"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/platform/platformtest"
)

func TestMain(m *testing.M) {
	// Decode the bundled data outside any synctest bubble: the decoder's goroutines must not
	// belong to one test's bubble.
	if _, err := data.Bundled(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// --- fakes ---

type fakeWindow struct {
	mu                   sync.Mutex
	visible, focused     bool
	x, y, w, h           int32
	zoom                 float64
	js                   []string
	frameless, alwaysTop bool
}

var _ Window = (*fakeWindow)(nil)

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
func (w *fakeWindow) SetAlwaysOnTop(on bool)       { w.mu.Lock(); w.alwaysTop = on; w.mu.Unlock() }
func (w *fakeWindow) SetFrameless(frameless bool) {
	w.mu.Lock()
	w.frameless = frameless
	w.mu.Unlock()
}
func (w *fakeWindow) SetSkipTaskbar(bool) {}
func (w *fakeWindow) Center()             {}

func (w *fakeWindow) isFrameless() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.frameless }
func (w *fakeWindow) zoomFactor() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.zoom
}
func (w *fakeWindow) scripts() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.js...)
}

type emitted struct {
	name    string
	payload any
}

type fakeDesktop struct {
	overlay, settings, notice fakeWindow

	mu       sync.Mutex
	cursor   [2]float64
	events   []emitted
	opened   []string
	tray     string
	raised   int                 // BringToFront calls that focus
	rounded  map[Window][3]int32 // last RoundWindow per window: width, height, radius
	anchored int
}

var _ Desktop = (*fakeDesktop)(nil)

func (d *fakeDesktop) Overlay() Window  { return &d.overlay }
func (d *fakeDesktop) Settings() Window { return &d.settings }
func (d *fakeDesktop) Notice() Window   { return &d.notice }
func (d *fakeDesktop) CursorPosition() (x, y float64, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cursor[0], d.cursor[1], true
}
func (d *fakeDesktop) MonitorAt(float64, float64) (Area, bool) {
	return Area{X: 0, Y: 0, Width: 1920, Height: 1080}, true
}
func (d *fakeDesktop) Emit(name string, payload any) {
	d.mu.Lock()
	d.events = append(d.events, emitted{name, payload})
	d.mu.Unlock()
}
func (d *fakeDesktop) OpenURL(u string) error {
	d.mu.Lock()
	d.opened = append(d.opened, u)
	d.mu.Unlock()
	return nil
}
func (d *fakeDesktop) BringToFront(w Window, _ *uint64, focus bool) error {
	if focus {
		w.Focus()
		d.mu.Lock()
		d.raised++
		d.mu.Unlock()
	}
	return nil
}
func (d *fakeDesktop) ShowInactive(w Window) { w.Show() }
func (d *fakeDesktop) RefocusGame(*uint64)   {}
func (d *fakeDesktop) RoundWindow(w Window, width, height, radius int32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rounded == nil {
		d.rounded = map[Window][3]int32{}
	}
	d.rounded[w] = [3]int32{width, height, radius}
}
func (d *fakeDesktop) MoveDirect(Window, int32, int32) error { return nil }
func (d *fakeDesktop) AnchorLayer(Window, bool, int32, int32) error {
	d.mu.Lock()
	d.anchored++
	d.mu.Unlock()
	return nil
}
func (d *fakeDesktop) RelabelTray(language string) { d.mu.Lock(); d.tray = language; d.mu.Unlock() }

func (d *fakeDesktop) setCursor(x, y float64) {
	d.mu.Lock()
	d.cursor = [2]float64{x, y}
	d.mu.Unlock()
}

func (d *fakeDesktop) roundedAs(w Window) [3]int32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rounded[w]
}

func (d *fakeDesktop) raises() int { d.mu.Lock(); defer d.mu.Unlock(); return d.raised }

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

func (d *fakeDesktop) openedURLs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string{}, d.opened...)
}

func game() platform.WindowInfo {
	return platformtest.GameWindow(platform.Rect{X: 0, Y: 0, Width: 1920, Height: 1080})
}

// newTestApp builds an app on fake platform parts and a fake desktop. Its background work
// stops when the test ends.
func newTestApp(t *testing.T, opts platformtest.Options) (*App, *fakeDesktop, platformtest.Handles) {
	t.Helper()
	return newTestAppOn(t, newTestState(t), opts)
}

// newTestAppOn builds the app on st. In a synctest bubble, build st outside it: its price
// sources' caches run goroutines for the life of the process.
func newTestAppOn(t *testing.T, st *State, opts platformtest.Options) (*App, *fakeDesktop, platformtest.Handles) {
	t.Helper()
	p, handles := platformtest.New(opts)
	st.platform = &p
	d := &fakeDesktop{cursor: [2]float64{500, 400}}
	return NewApp(NewFlow(t.Context(), st, d), nil), d, handles
}

func corpusText(t *testing.T, id string) string {
	t.Helper()
	text, err := coretest.CorpusText(id)
	require.NoError(t, err)
	return text
}

// --- tests ---

func TestNothingCopiedStaysQuiet(t *testing.T) {
	for _, e := range []error{platform.Timeout(500 * time.Millisecond), platform.ErrNoItemText} {
		assert.True(t, isSilentFailure(copyError(e)), e)
		assert.Equal(t, ipc.KindNoItem, ipc.From(copyError(e)).Kind)
	}
	backend := copyError(platform.Backend("xdo failed"))
	assert.Equal(t, ipc.KindPlatform, ipc.From(backend).Kind)
	assert.False(t, isSilentFailure(backend))
	assert.False(t, isSilentFailure(ipc.NewError(ipc.KindParse, "unknown item class")))
	assert.True(t, isSilentFailure(errBusy))
	assert.Equal(t, ipc.KindBusy, ipc.From(errBusy).Kind)
}

func TestPriceCheckCopiesParsesAndShowsTheOverlay(t *testing.T) {
	item := corpusText(t, "gloves/rare-stocky-mitts")
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game(), ItemText: item, AdvancedText: item})
		s := NewService(a)
		resp, err := s.PriceCheck(t.Context(), ipc.PriceCheckRequest{Pinned: true})
		require.NoError(t, err)
		assert.Equal(t, "Stocky Mitts", resp.Item.BaseType)
		assert.True(t, d.overlay.IsVisible())
		assert.True(t, a.st.pinned.Load())
		copied := d.named(ipc.EventItemCopied)
		require.Len(t, copied, 1)
		assert.True(t, copied[0].(ipc.ItemCopied).Pinned)
		snap := s.GetOverlaySnapshot()
		require.NotNil(t, snap)
		assert.Equal(t, resp.Item.BaseType, snap.Item.BaseType)
		assert.NotEmpty(t, h.InputSimulator.Sent(), "the copy chord was sent")
	})
}

func TestPriceCheckOutsideTheGameIsSilent(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game(), ItemText: "anything"})
		h.WindowTracker.SetFocused(false)
		_, _, err := a.runPriceCheck(t.Context(), false)
		require.ErrorIs(t, err, errNotFocused)
		assert.True(t, isSilentFailure(err))
		assert.False(t, d.overlay.IsVisible())
	})
}

func TestTriggerShowsParseFailures(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game(), ItemText: "Item Class: Nonsense\nRarity: Rare\n--------\nWhat"})
		a.Trigger(false)
		synctest.Wait()
		require.Len(t, d.named(ipc.EventPriceFailed), 1)
		assert.True(t, d.overlay.IsVisible())
	})
}

func TestTriggerDropsPressesWithinTheDebounce(t *testing.T) {
	item := corpusText(t, "gloves/rare-stocky-mitts")
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game(), ItemText: item, AdvancedText: item})
		a.Trigger(false)
		synctest.Wait()
		a.hideOverlay()
		a.Trigger(false) // within 150 ms of the first: dropped, silently
		synctest.Wait()
		assert.Len(t, d.named(ipc.EventItemCopied), 1)
		assert.Empty(t, d.named(ipc.EventPriceFailed))
		time.Sleep(flowGateDebounce)
		a.Trigger(false)
		synctest.Wait()
		assert.Len(t, d.named(ipc.EventItemCopied), 2)
	})
}

func TestQueryWithoutAnItemFails(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, _, _ := newTestAppOn(t, st, platformtest.Options{})
		_, err := NewService(a).QueryPrice(t.Context(), core.FilterSet{})
		assert.Equal(t, ipc.KindNoItem, ipc.From(err).Kind)
	})
}

func TestApplySettingsRollsBackHotkeysOnConflict(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game()})
		before := a.st.Settings()
		require.NoError(t, a.RegisterSettingsHotkeys())

		next := before.Clone()
		next.Hotkeys.PriceCheck = platform.NewChord("KeyK", platform.Ctrl)
		h.Hotkeys.Block(next.Hotkeys.PriceCheck)
		_, err := a.applySettings(next)
		require.Error(t, err)
		assert.Equal(t, before.Hotkeys.PriceCheck, h.Hotkeys.Registered()[platform.Action(platform.PriceCheck)], "rolled back")
		assert.Empty(t, d.named(ipc.EventSettingsChanged))
		assert.Equal(t, before.Hotkeys.PriceCheck, a.st.Settings().Hotkeys.PriceCheck)
		assert.Len(t, a.st.recentErrors(), 1)
	})
}

func TestApplySettingsSavesAndBroadcasts(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game()})
		paths := config.Under(t.TempDir())
		a.st.paths = &paths
		next := a.st.Settings()
		next.UI.Language = "de"
		next.Overlay.Scale = 1.25
		next.Chat.Commands = append(next.Chat.Commands, config.ChatCommand{Text: "/hideout", Send: true, Chord: platform.NewChord("F9", platform.Ctrl, platform.Shift)})
		saved, err := NewService(a).SaveSettings(t.Context(), next)
		require.NoError(t, err)
		assert.Equal(t, "de", saved.UI.Language)
		assert.Equal(t, "de", d.tray)
		assert.InDelta(t, 1.25, d.overlay.zoomFactor(), 1e-9)
		require.Len(t, d.named(ipc.EventSettingsChanged), 1)
		loaded, err := config.Load(paths)
		require.NoError(t, err)
		assert.Equal(t, "de", loaded.UI.Language)
		// A new chat command's chord is registered right away.
		idx := uint8(len(next.Chat.Commands) - 1)
		assert.Equal(t, "F9", h.Hotkeys.Registered()[platform.ChatAction(idx)].Key)
	})
}

func TestHotkeysFollowGameFocus(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, _, h := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.st.followFocus.Store(true)
		h.WindowTracker.SetFocused(false)
		require.NoError(t, a.RegisterSettingsHotkeys())
		assert.Empty(t, h.Hotkeys.Registered(), "released while the game is not focused")
		h.WindowTracker.SetFocused(true)
		a.SyncHotkeysToFocus()
		assert.NotEmpty(t, h.Hotkeys.Registered())
	})
}

func TestHotkeysStayWhileAnRKLWindowIsFocused(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.st.followFocus.Store(true)
		h.WindowTracker.SetFocused(false)
		d.overlay.Show()
		d.overlay.Focus() // the panel took focus from the game
		a.SyncHotkeysToFocus()
		assert.NotEmpty(t, h.Hotkeys.Registered(), "Ctrl+Alt+D must still close the focused panel")
		d.overlay.Hide()
		a.SyncHotkeysToFocus()
		assert.Empty(t, h.Hotkeys.Registered(), "released once neither the game nor Divvy has focus")
	})
}

func TestHotkeysAreRecheckedWhenAnRKLWindowTakesFocusLate(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.st.followFocus.Store(true)
		h.WindowTracker.SetFocused(false)
		a.st.overlayVisible.Store(true) // open, but the WM has not focused it yet
		a.SyncHotkeysToFocus()
		assert.Empty(t, h.Hotkeys.Registered())
		d.overlay.Focus()
		time.Sleep(focusRecheck)
		synctest.Wait()
		assert.NotEmpty(t, h.Hotkeys.Registered(), "grabbed again by the recheck")
	})
}

func TestOpenTradeRefusesOtherSites(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{})
		s := NewService(a)
		require.Error(t, s.OpenTrade("https://example.com/trade2"))
		require.NoError(t, s.OpenTrade("https://www.pathofexile.com/trade2/search/poe2/Standard/abc"))
		assert.Equal(t, []string{"https://www.pathofexile.com/trade2/search/poe2/Standard/abc"}, d.openedURLs())
	})
}

func TestRebindHotkeyRejectsMissingCommands(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, _, _ := newTestAppOn(t, st, platformtest.Options{})
		err := NewService(a).RebindHotkey(t.Context(), platform.ChatAction(40), platform.NewChord("F1"))
		assert.Equal(t, ipc.KindConfig, ipc.From(err).Kind)
	})
}

func TestSettingsCoverTheGameWhenTracked(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game()})
		s := NewService(a)
		require.NoError(t, s.OpenSettingsTab("hotkeys"))
		assert.True(t, d.settings.IsVisible())
		assert.True(t, d.settings.isFrameless())
		js := d.settings.scripts()
		require.Len(t, js, 1)
		assert.Contains(t, js[0], "dataset.overlay = '1'")
		assert.Contains(t, js[0], "#/settings?tab=hotkeys")
		require.Error(t, s.OpenSettingsTab("../x"))
	})
}

func TestSettingsAwayFromTheGameAreABorderlessRoundedWindow(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{})
		d.settings.SetSize(900, 680)
		a.ShowSettings("settings")
		assert.True(t, d.settings.IsVisible())
		assert.True(t, d.settings.isFrameless(), "borderless: the page draws its own title bar")
		assert.Contains(t, d.settings.scripts()[0], "dataset.overlay = '0'")
		assert.Equal(t, [3]int32{900, 680, 12}, d.roundedAs(&d.settings))

		// The window manager resized it: the rounded corners follow.
		d.settings.SetSize(1400, 900)
		a.SettingsResized()
		assert.Equal(t, [3]int32{1400, 900, 12}, d.roundedAs(&d.settings))
	})
}

func TestHotkeyLoopHidesAnOpenOverlay(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.RunHotkeyLoop(t.Context())
		d.overlay.Show()
		a.st.overlayVisible.Store(true)
		h.Hotkeys.Fire(platform.Action(platform.PriceCheck))
		synctest.Wait()
		assert.False(t, d.overlay.IsVisible())
	})
}

func TestEscFromTheHookClosesAnUnpinnedPanel(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.RunInputLoop(t.Context())
		d.overlay.Show()
		a.st.overlayVisible.Store(true)
		h.InputListener.Emit(platform.InputEvent{Type: platform.KeyDown, Key: "Escape"})
		synctest.Wait()
		assert.False(t, d.overlay.IsVisible())
	})
}

func TestNothingCopiedIsNotRecordedAsAnError(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, _, _ := newTestAppOn(t, st, platformtest.Options{Window: game()}) // the game copies nothing: the copy times out
		_, _, err := a.runPriceCheck(t.Context(), false)
		require.Error(t, err)
		assert.True(t, isSilentFailure(err))
		assert.Empty(t, a.st.recentErrors(), "no item under the cursor is not a diagnostics error")
	})
}

func TestFocusGuardTakesFocusBackFromTheGameAFewTimes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the focus guard runs only on Linux/X11 (startFocusGuard)")
	}
	item := corpusText(t, "gloves/rare-stocky-mitts")
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, h := newTestAppOn(t, st, platformtest.Options{Window: game(), ItemText: item, AdvancedText: item})
		_, _, err := a.handleItemText(item, false)
		require.NoError(t, err)
		assert.Equal(t, 1, d.raises(), "shown and focused")
		h.WindowTracker.SetFocused(true) // Wine keeps re-activating the game
		time.Sleep(focusGuard)
		synctest.Wait()
		assert.Equal(t, 1+maxRefocus, d.raises(), "re-focused at most maxRefocus times")
	})
}

func TestPinnedPanelIsFocusedWhenTheCursorRestsOnIt(t *testing.T) {
	item := corpusText(t, "gloves/rare-stocky-mitts")
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game(), ItemText: item, AdvancedText: item})
		_, _, err := a.handleItemText(item, true)
		require.NoError(t, err)
		d.overlay.Hide()
		d.overlay.Show() // the game took focus; the panel stays up
		a.st.overlayVisible.Store(true)
		raised := d.raises()
		x, y, _, _ := d.overlay.Bounds()
		d.setCursor(float64(x+10), float64(y+10))
		time.Sleep(hoverPoll + hoverFocusDelay + hoverPoll)
		synctest.Wait()
		assert.Equal(t, raised+1, d.raises(), "focused once")
		assert.True(t, d.overlay.IsFocused())
	})
}
