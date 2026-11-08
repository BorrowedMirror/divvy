package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/gamelog"
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

// Desktop is everything the flow needs from the windowing side: the windows, the cursor,
// monitors, events to the web views, the browser, and the per-OS window styling.
type Desktop interface {
	Overlay() Window
	Settings() Window
	// Notice is the ready notice shown over the game window when RKL finds it.
	Notice() Window
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

// silentError is a price check that ends without an item, which the overlay does not pop up
// for. The IPC layer reports it with its kind and message.
type silentError struct {
	kind ipc.Kind
	msg  string
}

var _ ipc.KindedError = silentError{}

func (e silentError) Error() string { return e.msg }

// IPCKind is the frontend error kind.
func (e silentError) IPCKind() ipc.Kind { return e.kind }

// Price checks that end without an item.
var (
	// errBusy is a press dropped because another flow is running (or debounced).
	errBusy = silentError{ipc.KindBusy, "a price check is already running"}
	// errNotFocused is a press outside the game.
	errNotFocused = silentError{ipc.KindNoItem, "game window is not focused"}
	// errNothingCopied is a copy that produced no item text: the cursor was over nothing, or
	// over something that copies as plain text.
	errNothingCopied = silentError{ipc.KindNoItem, "no item under the cursor"}
)

// isSilentFailure reports failures the overlay should not pop up for: dropped presses,
// presses outside the game and presses with no item under the cursor.
func isSilentFailure(err error) bool {
	_, ok := errors.AsType[silentError](err)
	return ok
}

// copyError turns a failed copy into the error the overlay sees. Nothing arriving, or text
// that is not an item, means there was no item under the cursor, so the panel stays closed.
// Real backend failures still show.
func copyError(err error) error {
	if errors.Is(err, platform.ErrTimeout) || errors.Is(err, platform.ErrNoItemText) {
		return errNothingCopied
	}
	return err
}

// Flow is the orchestration between platform, core, trade and the windows.
type Flow struct {
	// ctx is the app's lifetime: the flow's background work stops when it is done.
	ctx context.Context
	st  *State
	d   Desktop
	// noticeGen counts ready notices and dismissals, so a notice's hide timer leaves a newer
	// one alone.
	noticeGen atomic.Uint64
}

// NewFlow wires the flow to state and the desktop. Background work (focus guards, macros,
// price checks started without a caller) runs until ctx is done.
func NewFlow(ctx context.Context, st *State, d Desktop) *Flow {
	return &Flow{ctx: ctx, st: st, d: d}
}

// State is the app state the flow runs on.
func (f *Flow) State() *State { return f.st }

func (f *Flow) emit(event string, payload any) { f.d.Emit(event, payload) }

// sleep pauses for d; it reports false, early, when the app is shutting down.
func (f *Flow) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-f.ctx.Done():
		return false
	}
}

// goSafe runs fn on its own goroutine; a panic is recorded instead of taking the app down.
func (f *Flow) goSafe(what string, fn func()) {
	go func() {
		defer f.recoverPanic(what)
		fn()
	}()
}

// recoverPanic records a panic in one of the flow's goroutines instead of taking the app
// down. It must be deferred directly.
func (f *Flow) recoverPanic(what string) {
	if r := recover(); r != nil {
		f.st.RecordError(what+" panicked", fmt.Errorf("%v", r))
	}
}

// mayCopy reports whether the copy keystroke may be sent: only into the focused game window
// when the backend can track windows (otherwise e.g. a terminal would receive Ctrl+C).
// gameFocused is only called with tracking.
func mayCopy(windowTracking bool, gameFocused func() bool) bool {
	return !windowTracking || gameFocused()
}

// MustShowSettings reports whether settings must be shown at startup: only when the user would
// otherwise have no way in, neither a tray icon nor a registered hotkey. With either one the
// app is reachable (missing hotkeys are explained by first-run onboarding and the diagnostics
// page, and `rkl --trigger` works).
func MustShowSettings(trayOK, hotkeysOK bool) bool { return !trayOK && !hotkeysOK }

// handleItemText parses text, remembers it, emits `item-copied` and shows the overlay.
func (f *Flow) handleItemText(text string, pinned bool) (*core.ParsedItem, core.FilterSet, error) {
	st := f.st
	settings := st.Settings()
	opts := queryOptions(&settings, st.league())
	gameData := st.waitData()
	// A non-English client's item text is translated to English first; everything after
	// (parsing, matching, trade search, exports) works on the English text.
	var catalog core.Catalog
	if gameData != nil {
		catalog = gameData
		if t, ok := gameData.Translate(text); ok {
			slog.Info("item text translated", "language", t.Language)
			text = t.Text
		}
	}
	item, filters, err := processItemText(text, catalog, &opts)
	if err != nil {
		st.RecordError("item text", err)
		return nil, core.FilterSet{}, err
	}
	if settings.Trade.RememberFilters {
		st.applyFilterMemory(&filters)
	}
	st.setLastItem(copiedItem{item: *item, filters: filters, text: text})
	f.setPinned(pinned)
	f.showOverlay()
	f.emit(ipc.EventItemCopied, ipc.ItemCopied{Item: *item, Filters: filters, Pinned: pinned})
	return item, filters, nil
}

// runPriceCheck is the full hotkey flow: copy from the game, then [Flow.handleItemText].
func (f *Flow) runPriceCheck(ctx context.Context, pinned bool) (*core.ParsedItem, core.FilterSet, error) {
	st := f.st
	// One copy at a time: an overlapping run would capture the other's item text as the
	// "previous" clipboard and restore it. The permit is held until the copy has finished.
	permit, ok := st.gate.tryBegin(time.Now())
	if !ok {
		return nil, core.FilterSet{}, errBusy
	}
	defer permit.release()
	p := st.platform
	if p == nil {
		return nil, core.FilterSet{}, ipc.NewError(ipc.KindPlatform, "no platform backend available")
	}
	if !mayCopy(p.Capabilities.WindowTracking, p.WindowTracker.IsGameFocused) {
		return nil, core.FilterSet{}, errNotFocused
	}
	opts := platform.DefaultCopyOptions()
	opts.Advanced = st.Settings().Hotkeys.AdvancedCopy
	text, err := platform.CopyItemText(p.InputSimulator, p.Clipboard, opts)
	if err != nil {
		e := copyError(err)
		// Nothing under the cursor is a normal outcome, not an error worth keeping.
		if isSilentFailure(e) {
			slog.Debug("price check: nothing copied", "err", err)
		} else {
			st.RecordError("copy item", err)
		}
		return nil, core.FilterSet{}, e
	}
	if err := ctx.Err(); err != nil {
		return nil, core.FilterSet{}, err
	}
	return f.handleItemText(text, pinned)
}

// Trigger runs a price check without waiting (hotkey, signal, tray, test hook).
func (f *Flow) Trigger(pinned bool) {
	f.goSafe("price check", func() {
		_, _, err := f.runPriceCheck(f.ctx, pinned)
		switch {
		case err == nil:
			return
		case isSilentFailure(err) || f.ctx.Err() != nil:
			slog.Debug("price check skipped", "err", err)
			return
		}
		// Surface copy/parse failures in the overlay instead of failing silently.
		f.setPinned(pinned)
		f.showOverlay()
		f.emit(ipc.EventPriceFailed, ipc.PriceFailed{Error: *ipc.From(err)})
	})
}

// macroStepGap is the pause between macro steps, so the game keeps up.
const macroStepGap = 40 * time.Millisecond

// runMacro runs a chat command or stash search in the game: only while the game has focus, one
// at a time, on its own goroutine; the clipboard text it borrows for pasting is put back
// afterwards. steps runs there too (it may read the game log); an error is recorded and
// nothing is pressed.
func (f *Flow) runMacro(steps func() ([]macroStep, error)) {
	f.goSafe("macro", func() {
		st := f.st
		p := st.platform
		if p == nil || !mayCopy(p.Capabilities.WindowTracking, p.WindowTracker.IsGameFocused) {
			return
		}
		list, err := steps()
		if err != nil {
			st.RecordError("chat command", err)
			return
		}
		st.macroMu.Lock()
		defer st.macroMu.Unlock()
		previous, hadText, _ := p.Clipboard.Text()
		pasted := false
		for _, step := range list {
			var err error
			if step.paste {
				pasted = true
				if err = p.Clipboard.SetText(step.text); err == nil && f.sleep(macroStepGap) {
					err = p.InputSimulator.SendChord(platform.NewChord("KeyV", platform.Ctrl))
				}
			} else {
				err = p.InputSimulator.SendChord(step.chord)
			}
			if err != nil {
				st.RecordError("chat command / stash search", err)
				break
			}
			if !f.sleep(macroStepGap) {
				break
			}
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
			st.RecordError("restore clipboard", restoreErr)
		}
	})
}

// chatMacro builds a chat command's steps, with @last and @area read from the game log when
// it uses them.
func (f *Flow) chatMacro(text string, send bool) ([]macroStep, error) {
	wantLast, wantArea := chatPlaceholders(text)
	var ctx chatContext
	if wantLast || wantArea {
		settings := f.st.Settings()
		if path, ok := gamelog.Resolve(settings.Chat.GameLog); ok {
			vars, err := gamelog.Read(path, wantLast, wantArea)
			if err != nil {
				slog.Warn("read game log", "path", path, "err", err)
			}
			ctx = chatContext{last: vars.LastWhisper, area: vars.Area}
		}
	}
	return chatSteps(text, send, ctx)
}

// setPinned records the pinned flag. The panel takes focus in both modes; pinning only
// disables auto-hide and Esc-to-close.
func (f *Flow) setPinned(pinned bool) { f.st.pinned.Store(pinned) }

// emitOverlayState tells the web views whether the panel is visible and pinned.
func (f *Flow) emitOverlayState() {
	f.emit(ipc.EventOverlayState, ipc.OverlayState{Visible: f.st.overlayVisible.Load(), Pinned: f.st.pinned.Load()})
}

// ApplyPanelScale zooms the price panel's web view (the `overlay.scale` setting); window sizes
// follow it when the panel is laid out and resized.
func (f *Flow) ApplyPanelScale(scale float64) { f.d.Overlay().SetZoom(scale) }

// openTrade opens a trade-site URL in the browser, refusing anything that is not one.
func (f *Flow) openTrade(rawURL string) error {
	override, _ := envOverride("RKL_TRADE_BASE_URL")
	u, ok := tradeURL(rawURL, override)
	if !ok {
		return ipc.Errorf(ipc.KindInternal, "refusing to open non-trade url %s", rawURL)
	}
	if err := f.d.OpenURL(u); err != nil {
		return ipc.Errorf(ipc.KindInternal, "%w", err)
	}
	return nil
}

// macPoints reports whether tracker rects are logical points (macOS) rather than physical px.
var macPoints = runtime.GOOS == "darwin"
