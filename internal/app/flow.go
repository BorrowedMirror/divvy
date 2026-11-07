package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Window is one web view window as the flow uses it. The shell implements it over Wails.
// Positions and sizes are physical pixels unless the method says logical.
type Window interface {
	Show()
	Hide()
	Focus()
	IsVisible() bool
	IsFocused() bool
	// ScaleFactor is physical pixels per logical pixel on the window's monitor.
	ScaleFactor() float64
	Bounds() (x, y, w, h int32)
	SetPosition(x, y int32)
	SetSize(w, h int32)
	// SetLogicalBounds places the window in logical units (macOS points).
	SetLogicalBounds(x, y, w, h float64)
	SetLogicalSize(w, h float64)
	SetZoom(zoom float64)
	ExecJS(js string)
	SetAlwaysOnTop(on bool)
	SetFrameless(frameless bool)
	SetSkipTaskbar(skip bool)
	Center()
}

// Desktop is everything the flow needs from the windowing side: the two windows, the cursor,
// monitors, events to the web views, the browser, and the per-OS window styling.
type Desktop interface {
	Overlay() Window
	Settings() Window
	// CursorPosition is the cursor in physical px.
	CursorPosition() (x, y float64, ok bool)
	// MonitorAt is the monitor under a point (physical px), else the primary monitor.
	MonitorAt(x, y float64) (Area, bool)
	Emit(event string, payload any)
	OpenURL(url string) error
	// BringToFront stacks w above the game (gameID, when tracked) and optionally focuses it,
	// bypassing focus-stealing prevention where the platform allows it.
	BringToFront(w Window, gameID *uint64, focus bool) error
	// RefocusGame hands keyboard focus back to the game after the panel closes.
	RefocusGame(gameID *uint64)
	// RoundWindow gives w rounded corners (radius in physical px); width 0 removes the shape.
	RoundWindow(w Window, width, height, radius int32)
	// MoveDirect moves w without the toolkit's "already there" shortcut (X11).
	MoveDirect(w Window, x, y int32) error
	// AnchorLayer anchors a layer-shell window to an output edge (Wayland-native).
	AnchorLayer(w Window, right bool, top, side int32) error
	RelabelTray(language string)
}

// Overlay card corner radius (logical px), matching the CSS.
const cardRadius = 12.0

// Error kinds and messages the frontend tells apart.
const (
	// KindBusy is a press dropped because another flow is running (or debounced).
	KindBusy = "busy"
	// NotFocused is the `no_item` message when the hotkey fired outside the game.
	NotFocused = "game window is not focused"
	// NothingCopied is the `no_item` message when the copy produced no item text (the cursor
	// was over nothing, or over something that copies as plain text).
	NothingCopied = "no item under the cursor"
)

// Flow is the orchestration between platform, core, trade and the windows.
type Flow struct {
	st *State
	d  Desktop
	// sleep is time.Sleep, replaceable in tests.
	sleep func(time.Duration)
}

// NewFlow wires the flow to state and the desktop.
func NewFlow(st *State, d Desktop) *Flow {
	return &Flow{st: st, d: d, sleep: time.Sleep}
}

// State is the app state the flow runs on.
func (f *Flow) State() *State { return f.st }

func (f *Flow) emit(event string, payload any) { f.d.Emit(event, payload) }

// HandleItemText parses text, remembers it, emits `item-copied` and shows the overlay.
func (f *Flow) HandleItemText(text string, pinned bool) (*core.ParsedItem, core.FilterSet, error) {
	st := f.st
	settings := st.Settings()
	opts := QueryOptionsFor(&settings, st.League())
	gameData := st.WaitData()
	// A non-English client's item text is translated to English first; everything after
	// (parsing, matching, trade search, exports) works on the English text.
	var catalog core.Catalog
	if gameData != nil {
		catalog = gameData
		if t := gameData.Translate(text); t != nil {
			slog.Info("item text translated", "language", t.Language)
			text = t.Text
		}
	}
	item, filters, err := ProcessItemText(text, catalog, &opts)
	if err != nil {
		st.RecordError(err)
		return nil, core.FilterSet{}, err
	}
	if settings.Trade.RememberFilters {
		st.ApplyFilterMemory(&filters)
	}
	st.SetLastItem(*item, filters, new(text))
	f.SetPinned(pinned)
	f.ShowOverlay()
	f.emit(ipc.EventItemCopied, ipc.ItemCopied{Item: *item, Filters: filters, Pinned: pinned})
	return item, filters, nil
}

// macroStepGap is the pause between macro steps, so the game keeps up.
const macroStepGap = 40 * time.Millisecond

// RunMacro runs a chat command or stash search in the game: only while the game has focus, one
// at a time, on its own goroutine; the clipboard text it borrows for pasting is put back
// afterwards. steps runs there too (it may read the game log); an error is recorded and
// nothing is pressed.
func (f *Flow) RunMacro(steps func() ([]MacroStep, error)) {
	go func() {
		defer f.recoverPanic("macro")
		st := f.st
		p := st.Platform
		if p == nil || !MayCopy(p.Capabilities.WindowTracking, p.WindowTracker.IsGameFocused) {
			return
		}
		list, err := steps()
		if err != nil {
			st.RecordError(fmt.Sprintf("chat command: %v", err))
			return
		}
		st.MacroLock.Lock()
		defer st.MacroLock.Unlock()
		previous, hadText, _ := p.Clipboard.Text()
		pasted := false
		for _, step := range list {
			var err error
			if step.IsPaste {
				pasted = true
				if err = p.Clipboard.SetText(step.Text); err == nil {
					f.sleep(macroStepGap)
					err = p.InputSimulator.SendChord(platform.NewChord("KeyV", platform.Ctrl))
				}
			} else {
				err = p.InputSimulator.SendChord(step.Chord)
			}
			if err != nil {
				st.RecordError(fmt.Sprintf("chat command / stash search: %v", err))
				break
			}
			f.sleep(macroStepGap)
		}
		if !pasted {
			return
		}
		f.sleep(150 * time.Millisecond)
		var restoreErr error
		switch {
		case hadText:
			restoreErr = p.Clipboard.SetText(previous)
		case !p.Clipboard.HasNonText():
			restoreErr = p.Clipboard.Clear()
		}
		if restoreErr != nil {
			st.RecordError(fmt.Sprintf("restore clipboard: %v", restoreErr))
		}
	}()
}

// chatMacro builds a chat command's steps, with @last and @area read from the game log when
// it uses them.
func (f *Flow) chatMacro(text string, send bool) ([]MacroStep, error) {
	wantLast, wantArea := ChatPlaceholders(text)
	var ctx ChatContext
	if wantLast || wantArea {
		settings := f.st.Settings()
		if path, ok := ResolveLogPath(settings.Chat.GameLog); ok {
			vars, err := ReadLogVars(path, wantLast, wantArea)
			if err != nil {
				slog.Warn("read game log", "path", path, "err", err)
			} else {
				if vars.LastWhisper != "" {
					ctx.Last = new(vars.LastWhisper)
				}
				if vars.Area != "" {
					ctx.Area = new(vars.Area)
				}
			}
		}
	}
	return ChatSteps(text, send, ctx)
}

// RunPriceCheck is the full hotkey flow: copy from the game, then [Flow.HandleItemText].
func (f *Flow) RunPriceCheck(ctx context.Context, pinned bool) (*core.ParsedItem, core.FilterSet, error) {
	st := f.st
	// One copy at a time: an overlapping run would capture the other's item text as the
	// "previous" clipboard and restore it. The permit is held until the copy has finished.
	permit, ok := st.FlowGate.TryBegin(time.Now())
	if !ok {
		return nil, core.FilterSet{}, ipc.NewError(KindBusy, "a price check is already running")
	}
	defer permit.Release()
	p := st.Platform
	if p == nil {
		return nil, core.FilterSet{}, ipc.NewError("platform", "no platform backend available")
	}
	if !MayCopy(p.Capabilities.WindowTracking, p.WindowTracker.IsGameFocused) {
		return nil, core.FilterSet{}, ipc.NewError("no_item", NotFocused)
	}
	opts := platform.DefaultCopyOptions()
	opts.Advanced = st.Settings().Hotkeys.AdvancedCopy
	text, err := platform.CopyItemText(p.InputSimulator, p.Clipboard, opts, f.sleep)
	if err != nil {
		e := CopyError(err)
		// Nothing under the cursor is a normal outcome, not an error worth keeping.
		if IsSilentFailure(e) {
			slog.Debug("price check: nothing copied", "err", err)
		} else {
			st.RecordError(err)
		}
		return nil, core.FilterSet{}, e
	}
	if err := ctx.Err(); err != nil {
		return nil, core.FilterSet{}, err
	}
	return f.HandleItemText(text, pinned)
}

// CopyError turns a failed copy into the IpcError the overlay sees. Nothing arriving, or text
// that is not an item, means there was no item under the cursor, so the panel stays closed.
// Real backend failures still show.
func CopyError(err error) *ipc.IpcError {
	if errors.Is(err, platform.ErrTimeout) || errors.Is(err, platform.ErrNoItemText) {
		return ipc.NewError("no_item", NothingCopied)
	}
	return ipc.From(err)
}

// IsSilentFailure reports failures the overlay should not pop up for: dropped presses,
// presses outside the game and presses with no item under the cursor.
func IsSilentFailure(e *ipc.IpcError) bool {
	return e.Kind == KindBusy || (e.Kind == "no_item" && (e.Message == NotFocused || e.Message == NothingCopied))
}

// Trigger runs a price check without waiting (hotkey, signal, tray, test hook).
func (f *Flow) Trigger(pinned bool) {
	go func() {
		defer f.recoverPanic("price check")
		_, _, err := f.RunPriceCheck(context.Background(), pinned)
		if err == nil {
			return
		}
		e := ipc.From(err)
		if IsSilentFailure(e) {
			slog.Debug("price check skipped", "err", e)
			return
		}
		// Surface copy/parse failures in the overlay instead of failing silently.
		f.SetPinned(pinned)
		f.ShowOverlay()
		f.emit(ipc.EventPriceFailed, ipc.PriceFailed{Error: *e})
	}()
}

// SetPinned records the pinned flag. The panel takes focus in both modes; pinning only
// disables auto-hide and Esc-to-close.
func (f *Flow) SetPinned(pinned bool) { f.st.Pinned.Store(pinned) }

// EmitOverlayState tells the web views whether the panel is visible and pinned.
func (f *Flow) EmitOverlayState() {
	f.emit(ipc.EventOverlayState, ipc.OverlayState{Visible: f.st.OverlayVisible.Load(), Pinned: f.st.IsPinned()})
}

// ApplyPanelScale zooms the price panel's web view (the `overlay.scale` setting); window sizes
// follow it in layoutOverlay and ResizeOverlay.
func (f *Flow) ApplyPanelScale(scale float64) { f.d.Overlay().SetZoom(scale) }

// CardBounds is where a card may sit over the game: the whole game window, docks' strips
// included (like the fullscreen game itself).
func CardBounds(game platform.Rect) Area {
	return Area{X: game.X, Y: game.Y, Width: int32(game.Width), Height: int32(game.Height)}
}

// macPoints reports whether tracker rects are logical points (macOS) rather than physical px.
var macPoints = runtime.GOOS == "darwin"

func physical(v, scale float64) int32 { return int32(math.Round(v * scale)) }

// reassertCardPosition repeats a card-sized window's position shortly after it is shown: X11
// WMs (openbox) keep a newly mapped window out of docks' reserved strips (polybar), so a card
// placed at the very top opens below the dock, but once the window is mapped a move is honoured.
func (f *Flow) reassertCardPosition() {
	if runtime.GOOS != "linux" || f.st.Backend == platform.BackendWayland {
		return
	}
	seq := f.st.ShowSeq.Load()
	go func() {
		for _, delay := range []time.Duration{60 * time.Millisecond, 250 * time.Millisecond} {
			f.sleep(delay)
			st := f.st
			if st.ShowSeq.Load() != seq || !st.OverlayVisible.Load() || st.OverlayLayout().Cover || st.dragging() {
				return
			}
			card, _ := st.Card()
			// Straight to X11: GTK skips a move to where it believes the window already is.
			if err := f.d.MoveDirect(f.d.Overlay(), card.X, card.Y); err != nil {
				slog.Debug("reassert card position", "err", err)
			}
		}
	}()
}

// roundCard rounds a card-sized panel window of width x height logical px.
func (f *Flow) roundCard(w Window, width, height, zoom float64) {
	scale := w.ScaleFactor()
	px := func(v float64) int32 { return max(physical(v, scale), 0) }
	f.d.RoundWindow(w, px(width), px(height), px(cardRadius*zoom))
}

// layoutOverlay lays the price panel out with its card at card (screen, physical px).
//
// Unpinned over a tracked game window, the window covers the game (dimmed backdrop, the card
// drawn inside), so the pointer never enters the game and focus-follows-mouse WMs (openbox)
// cannot hand focus back to it; a backdrop click closes the panel. Pinned (or without a
// tracked game), the window is just the card, so the game stays usable around it.
func (f *Flow) layoutOverlay(w Window, card Point) {
	st := f.st
	scale := w.ScaleFactor()
	zoom := st.Settings().Overlay.Scale
	_, cardHeight := st.Card()
	st.SetCardPosition(card)
	var layout ipc.OverlayLayout
	if g, ok := f.GameWindowIfUnpinned(); ok {
		f.coverGame(w, g.Rect)
		x, y := CardInWindow(card.X, card.Y, g.Rect, scale, macPoints)
		f.d.RoundWindow(w, 0, 0, 0)
		// The web view is zoomed by the panel scale: its CSS pixels are `zoom` logical pixels.
		layout = ipc.OverlayLayout{Cover: true, X: x / zoom, Y: y / zoom}
	} else {
		width := st.CardCSSWidth() * zoom
		w.SetLogicalSize(width, cardHeight)
		f.roundCard(w, width, cardHeight, zoom)
		w.SetPosition(card.X, card.Y)
		layout = ipc.OverlayLayout{}
	}
	st.SetOverlayLayout(layout)
	f.emit(ipc.EventOverlayLayout, layout)
}

// GameWindowIfUnpinned is the tracked game window, unless the panel is pinned.
func (f *Flow) GameWindowIfUnpinned() (platform.WindowInfo, bool) {
	if f.st.IsPinned() {
		return platform.WindowInfo{}, false
	}
	return f.GameWindow()
}

// ShowOverlay positions the panel beside the cursor and shows it.
func (f *Flow) ShowOverlay() {
	st := f.st
	w := f.d.Overlay()
	scale := w.ScaleFactor()
	card, cardHeight := st.Card()
	if cx, cy, ok := f.d.CursorPosition(); ok {
		settings := st.Settings()
		zoom := settings.Overlay.Scale
		sizeW := physical(st.CardCSSWidth()*zoom, scale)
		sizeH := physical(cardHeight, scale)
		if screen, ok := f.d.MonitorAt(cx, cy); ok {
			// Edges are the game window's when it is tracked (physical px, not macOS points).
			// The whole monitor otherwise, docks included: like a fullscreen game, the card may
			// sit right at the top.
			area := screen
			if g, ok := f.GameWindow(); ok && !macPoints {
				area = Intersect(CardBounds(g.Rect), screen)
			}
			side := settings.Overlay.Side
			var x, y int32
			if last, ok := st.LastPosition(); side == config.SideLast && ok {
				x, y = ClampInto(last.X, last.Y, sizeW, sizeH, screen)
			} else {
				x, y = PanelPosition(side, int32(cx), int32(cy), sizeW, sizeH, area)
			}
			// Layer-shell surfaces ignore positions; anchor them to the output edge instead.
			if st.Backend == platform.BackendWayland {
				right := x > area.X+area.Width/2
				edge := x - area.X
				if right {
					edge = area.X + area.Width - (x + sizeW)
				}
				if err := f.d.AnchorLayer(w, right, y-area.Y, max(edge, 0)); err != nil {
					st.RecordError(fmt.Sprintf("layer-shell anchor: %v", err))
				}
			}
			card = Point{x, y}
		}
	}
	st.CardMoved.Store(false)
	f.layoutOverlay(w, card)
	w.Show()
	// A focused fullscreen game can sit above "always on top" windows (openbox and other X11
	// WMs), so explicitly stack the panel above the game and give it keyboard focus (Esc,
	// scrolling, filter editing). HideOverlay hands focus back to the game.
	gameID := f.gameID()
	if err := f.d.BringToFront(w, gameID, true); err != nil {
		st.RecordError(fmt.Sprintf("overlay raise: %v", err))
	}
	st.OverlayVisible.Store(true)
	f.startFocusGuard(gameID)
	// After the focus guard, which bumps ShowSeq for this show.
	f.reassertCardPosition()
	f.startPinnedHover(gameID)
	f.EmitOverlayState()
}

// HideOverlay hides the panel and gives the game its focus back.
func (f *Flow) HideOverlay() error {
	w := f.d.Overlay()
	hadFocus := w.IsFocused()
	w.Hide()
	if hadFocus {
		f.d.RefocusGame(f.gameID())
	}
	f.st.OverlayVisible.Store(false)
	f.SetPinned(false)
	f.EmitOverlayState()
	return nil
}

func (f *Flow) gameID() *uint64 {
	if g, ok := f.GameWindow(); ok {
		return new(g.NativeID)
	}
	return nil
}

const (
	// focusGuard is how long after showing the panel the focus guard watches for the game
	// taking focus back.
	focusGuard = 1500 * time.Millisecond
	// maxRefocus caps re-focus attempts per show (avoid a focus tug-of-war with the game).
	maxRefocus = 3
)

// startFocusGuard (Linux/X11): Wine re-activates a fullscreen game shortly after it loses
// focus, and holds an X pointer grab while it is active. Keep focus on the panel for a moment
// so Wine releases it.
func (f *Flow) startFocusGuard(gameID *uint64) {
	if runtime.GOOS != "linux" || f.st.Backend != platform.BackendX11 || gameID == nil {
		return
	}
	seq := f.st.ShowSeq.Add(1)
	go func() {
		defer f.recoverPanic("focus guard")
		refocused := 0
		for start := time.Now(); time.Since(start) < focusGuard; {
			f.sleep(100 * time.Millisecond)
			st := f.st
			if st.ShowSeq.Load() != seq || !st.OverlayVisible.Load() {
				return
			}
			// A pinned panel lets the game have focus (hover it, focus-follows-mouse); only the
			// unpinned panel, which covers the game, is kept focused against Wine re-activating it.
			if st.Platform.WindowTracker.IsGameFocused() && !st.IsPinned() && refocused < maxRefocus {
				refocused++
				slog.Debug("game took focus back; re-focusing the panel", "attempt", refocused)
				_ = f.d.BringToFront(f.d.Overlay(), gameID, true)
			}
		}
	}()
}

const (
	// hoverFocusDelay is how long the cursor must rest on a pinned panel before it is focused.
	hoverFocusDelay = 150 * time.Millisecond
	hoverPoll       = 50 * time.Millisecond
)

// startPinnedHover: a pinned panel does not cover the game, and while a Proton/Wine game is
// focused it holds an X pointer grab, so the WM never sees the pointer enter the panel and
// focus-follows-mouse cannot focus it. Poll the cursor (the query works under a grab) and
// activate the panel when the cursor rests on it; Wine then releases its grab.
func (f *Flow) startPinnedHover(gameID *uint64) {
	if gameID == nil || !f.st.IsPinned() {
		return
	}
	seq := f.st.ShowSeq.Load()
	go func() {
		defer f.recoverPanic("pinned hover")
		var insideSince time.Time
		for {
			f.sleep(hoverPoll)
			st := f.st
			if st.ShowSeq.Load() != seq || !st.OverlayVisible.Load() || !st.IsPinned() {
				return
			}
			w := f.d.Overlay()
			cx, cy, ok := f.d.CursorPosition()
			if !ok {
				continue
			}
			x, y, wd, ht := w.Bounds()
			if !PointInRect(cx, cy, float64(x), float64(y), float64(wd), float64(ht)) {
				insideSince = time.Time{}
				continue
			}
			if insideSince.IsZero() {
				insideSince = time.Now()
			}
			if time.Since(insideSince) >= hoverFocusDelay && !w.IsFocused() {
				if err := f.d.BringToFront(w, gameID, true); err != nil {
					st.RecordError(fmt.Sprintf("focus pinned panel: %v", err))
				}
				// Once per hover: leave and come back to focus it again.
				insideSince = time.Now().Add(time.Hour)
			}
		}
	}()
}

// GameWindow is the tracked game window, when the backend can track it (not on
// Wayland-native).
func (f *Flow) GameWindow() (platform.WindowInfo, bool) {
	p := f.st.Platform
	if p == nil || !p.Capabilities.WindowTracking || f.st.Backend == platform.BackendWayland {
		return platform.WindowInfo{}, false
	}
	g, ok := p.WindowTracker.GameWindow()
	if !ok || g.Rect.Width == 0 || g.Rect.Height == 0 {
		return platform.WindowInfo{}, false
	}
	return g, true
}

// coverGame moves and resizes w to exactly cover the game window. Tracker rects are physical
// pixels everywhere except macOS, where Core Graphics reports points.
func (f *Flow) coverGame(w Window, r platform.Rect) {
	if macPoints {
		w.SetLogicalBounds(float64(r.X), float64(r.Y), float64(r.Width), float64(r.Height))
		return
	}
	w.SetPosition(r.X, r.Y)
	w.SetSize(int32(r.Width), int32(r.Height))
}

// settingsSize is the settings window's size when it is not bound to the game.
const settingsWidth, settingsHeight = 900.0, 680.0

// ShowSettings shows the settings window on route (`settings`, `diagnostics`, `onboarding`).
//
// With a tracked game window the settings become an overlay that covers the game exactly
// (undecorated, transparent backdrop, card centred, kept above the game); otherwise it is a
// normal decorated window.
func (f *Flow) ShowSettings(route string) error {
	st := f.st
	w := f.d.Settings()
	g, overlay := f.GameWindow()
	wasOverlay := st.SettingsOverlay.Swap(overlay)
	if overlay {
		w.SetFrameless(true)
		w.SetSkipTaskbar(true)
		f.coverGame(w, g.Rect)
	} else if wasOverlay {
		w.SetAlwaysOnTop(false)
		w.SetSkipTaskbar(false)
		w.SetFrameless(false)
		w.SetLogicalSize(settingsWidth, settingsHeight)
		w.Center()
	}
	flag := "0"
	if overlay {
		flag = "1"
	}
	w.ExecJS(fmt.Sprintf("document.documentElement.dataset.overlay = '%s'; window.location.hash = '#/%s'", flag, route))
	w.Show()
	if overlay {
		if err := f.d.BringToFront(w, new(g.NativeID), true); err != nil {
			st.RecordError(fmt.Sprintf("settings raise: %v", err))
		}
	} else {
		w.Focus()
	}
	return nil
}

// HideSettings hides the settings window (Esc / backdrop click in overlay mode, close button).
func (f *Flow) HideSettings() error {
	f.d.Settings().Hide()
	return nil
}

// OpenTrade opens a trade-site URL in the browser, refusing anything that is not one.
func (f *Flow) OpenTrade(rawURL string) error {
	override, _ := EnvOverride("RKL_TRADE_BASE_URL")
	if !IsAllowedTradeURL(rawURL, override) {
		return ipc.Errorf("internal", "refusing to open non-trade url %s", rawURL)
	}
	if err := f.d.OpenURL(rawURL); err != nil {
		return ipc.NewError("internal", err.Error())
	}
	return nil
}

// CopyChord is the copy chord for the current settings (`Ctrl+C` / `Ctrl+Alt+C`).
func CopyChord(advanced bool) platform.Chord {
	if advanced {
		return platform.NewChord("KeyC", platform.Ctrl, platform.Alt)
	}
	return platform.NewChord("KeyC", platform.Ctrl)
}

// IsCloseKey reports key names that close an unpinned overlay when seen by the passive hook.
func IsCloseKey(key string) bool { return key == "Escape" || key == "Esc" }

func (s *State) dragging() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drag != nil
}

// recoverPanic logs a panic in one of the flow's goroutines instead of taking the app down.
func (f *Flow) recoverPanic(what string) {
	if r := recover(); r != nil {
		f.st.RecordError(fmt.Sprintf("%s panicked: %v", what, r))
	}
}
