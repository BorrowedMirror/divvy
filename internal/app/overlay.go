package app

import (
	"log/slog"
	"math"
	"runtime"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// The price panel: placing the card beside the cursor, covering the game while unpinned,
// keeping focus against the game, resizing and dragging.

// gameWindow is the tracked game window, when the backend can track it (not on
// Wayland-native).
func (f *Flow) gameWindow() (platform.WindowInfo, bool) {
	p := f.st.platform
	if p == nil || !p.Capabilities.WindowTracking || f.st.backend == platform.BackendWayland {
		return platform.WindowInfo{}, false
	}
	g, ok := p.WindowTracker.GameWindow()
	if !ok || g.Rect.Width == 0 || g.Rect.Height == 0 {
		return platform.WindowInfo{}, false
	}
	return g, true
}

// gameWindowIfUnpinned is the tracked game window, unless the panel is pinned.
func (f *Flow) gameWindowIfUnpinned() (platform.WindowInfo, bool) {
	if f.st.pinned.Load() {
		return platform.WindowInfo{}, false
	}
	return f.gameWindow()
}

func (f *Flow) gameID() *uint64 {
	if g, ok := f.gameWindow(); ok {
		return new(g.NativeID)
	}
	return nil
}

// coverGame moves and resizes w to exactly cover the game window. Tracker rects are physical
// pixels everywhere except macOS, where Core Graphics reports points.
func coverGame(w Window, r platform.Rect) {
	if macPoints {
		w.SetLogicalBounds(float64(r.X), float64(r.Y), float64(r.Width), float64(r.Height))
		return
	}
	w.SetPosition(r.X, r.Y)
	w.SetSize(int32(r.Width), int32(r.Height))
}

// roundCard rounds a card-sized panel window of width x height logical px.
func (f *Flow) roundCard(w Window, width, height, zoom float64) {
	scale := w.ScaleFactor()
	px := func(v float64) int32 { return max(physical(v, scale), 0) }
	f.d.RoundWindow(w, px(width), px(height), px(cardRadius*zoom))
}

// coverLayout is the layout of a window covering game with the card at card (screen,
// physical px). The web view is zoomed by the panel scale: its CSS pixels are zoom logical
// pixels.
func coverLayout(card point, game platform.Rect, scale, zoom float64) ipc.OverlayLayout {
	x, y := cardInWindow(card.X, card.Y, game, scale, macPoints)
	return ipc.OverlayLayout{Cover: true, X: x / zoom, Y: y / zoom}
}

// setLayout stores and broadcasts the panel layout.
func (f *Flow) setLayout(layout ipc.OverlayLayout) {
	f.st.setLayout(layout)
	f.emit(ipc.EventOverlayLayout, layout)
}

// layoutOverlay lays the price panel out with its card at card (screen, physical px).
//
// Unpinned over a tracked game window, the window covers the game (dimmed backdrop, the card
// drawn inside), so the pointer never enters the game and focus-follows-mouse WMs (openbox)
// cannot hand focus back to it; a backdrop click closes the panel. Pinned (or without a
// tracked game), the window is just the card, so the game stays usable around it.
func (f *Flow) layoutOverlay(w Window, card point) {
	st := f.st
	zoom := st.Settings().Overlay.Scale
	_, height := st.cardPosition()
	st.setCardPosition(card)
	if g, ok := f.gameWindowIfUnpinned(); ok {
		coverGame(w, g.Rect)
		f.d.RoundWindow(w, 0, 0, 0)
		f.setLayout(coverLayout(card, g.Rect, w.ScaleFactor(), zoom))
		return
	}
	width := st.cardWidthCSS() * zoom
	w.SetLogicalSize(width, height)
	f.roundCard(w, width, height, zoom)
	w.SetPosition(card.X, card.Y)
	f.setLayout(ipc.OverlayLayout{})
}

// showOverlay positions the panel beside the cursor and shows it.
func (f *Flow) showOverlay() {
	f.dismissNotice()
	st := f.st
	w := f.d.Overlay()
	scale := w.ScaleFactor()
	card, height := st.cardPosition()
	if cx, cy, ok := f.d.CursorPosition(); ok {
		settings := st.Settings()
		sizeW := physical(st.cardWidthCSS()*settings.Overlay.Scale, scale)
		sizeH := physical(height, scale)
		if screen, ok := f.d.MonitorAt(cx, cy); ok {
			// Edges are the game window's when it is tracked (physical px, not macOS points).
			// The whole monitor otherwise, docks included: like a fullscreen game, the card may
			// sit right at the top.
			area := screen
			if g, ok := f.gameWindow(); ok && !macPoints {
				area = intersect(cardBounds(g.Rect), screen)
			}
			side := settings.Overlay.Side
			var x, y int32
			if last, ok := st.droppedPosition(); side == config.SideLast && ok {
				x, y = clampInto(last.X, last.Y, sizeW, sizeH, screen)
			} else {
				x, y = panelPosition(side, int32(cx), int32(cy), sizeW, sizeH, area)
			}
			// Layer-shell surfaces ignore positions; anchor them to the output edge instead.
			if st.backend == platform.BackendWayland {
				right := x > area.X+area.Width/2
				edge := x - area.X
				if right {
					edge = area.X + area.Width - (x + sizeW)
				}
				if err := f.d.AnchorLayer(w, right, y-area.Y, max(edge, 0)); err != nil {
					st.RecordError("layer-shell anchor", err)
				}
			}
			card = point{x, y}
		}
	}
	st.cardMoved.Store(false)
	f.layoutOverlay(w, card)
	w.Show()
	// A focused fullscreen game can sit above "always on top" windows (openbox and other X11
	// WMs), so explicitly stack the panel above the game and give it keyboard focus (Esc,
	// scrolling, filter editing). hideOverlay hands focus back to the game.
	gameID := f.gameID()
	if err := f.d.BringToFront(w, gameID, true); err != nil {
		st.RecordError("overlay raise", err)
	}
	st.overlayVisible.Store(true)
	seq := st.showSeq.Add(1)
	f.startFocusGuard(seq, gameID)
	f.reassertCardPosition(seq)
	f.startPinnedHover(seq, gameID)
	f.emitOverlayState()
}

// hideOverlay hides the panel and gives the game its focus back.
func (f *Flow) hideOverlay() {
	w := f.d.Overlay()
	hadFocus := w.IsFocused()
	w.Hide()
	if hadFocus {
		f.d.RefocusGame(f.gameID())
	}
	f.st.overlayVisible.Store(false)
	f.setPinned(false)
	f.emitOverlayState()
}

// shown reports whether the panel is still showing the show numbered seq.
func (f *Flow) shown(seq uint64) bool {
	return f.st.showSeq.Load() == seq && f.st.overlayVisible.Load()
}

// reassertCardPosition repeats a card-sized window's position shortly after it is shown: X11
// WMs (openbox) keep a newly mapped window out of docks' reserved strips (polybar), so a card
// placed at the very top opens below the dock, but once the window is mapped a move is honoured.
func (f *Flow) reassertCardPosition(seq uint64) {
	if runtime.GOOS != "linux" || f.st.backend == platform.BackendWayland {
		return
	}
	f.goSafe("reassert card position", func() {
		st := f.st
		for _, delay := range []time.Duration{60 * time.Millisecond, 250 * time.Millisecond} {
			if !f.sleep(delay) || !f.shown(seq) || st.layout().Cover || st.dragging() {
				return
			}
			card, _ := st.cardPosition()
			// Straight to X11: GTK skips a move to where it believes the window already is.
			if err := f.d.MoveDirect(f.d.Overlay(), card.X, card.Y); err != nil {
				slog.Debug("reassert card position", "err", err)
			}
		}
	})
}

const (
	// focusGuard is how long after showing the panel the focus guard watches for the game
	// taking focus back.
	focusGuard = 1500 * time.Millisecond
	// focusPoll is how often the focus guard looks.
	focusPoll = 100 * time.Millisecond
	// maxRefocus caps re-focus attempts per show (avoid a focus tug-of-war with the game).
	maxRefocus = 3
)

// startFocusGuard (Linux/X11): Wine re-activates a fullscreen game shortly after it loses
// focus, and holds an X pointer grab while it is active. Keep focus on the panel for a moment
// so Wine releases it.
func (f *Flow) startFocusGuard(seq uint64, gameID *uint64) {
	if runtime.GOOS != "linux" || f.st.backend != platform.BackendX11 || gameID == nil {
		return
	}
	f.goSafe("focus guard", func() {
		st := f.st
		refocused := 0
		for range focusGuard / focusPoll {
			if !f.sleep(focusPoll) || !f.shown(seq) {
				return
			}
			// A pinned panel lets the game have focus (hover it, focus-follows-mouse); only the
			// unpinned panel, which covers the game, is kept focused against Wine re-activating it.
			if st.platform.WindowTracker.IsGameFocused() && !st.pinned.Load() && refocused < maxRefocus {
				refocused++
				slog.Debug("game took focus back; re-focusing the panel", "attempt", refocused)
				if err := f.d.BringToFront(f.d.Overlay(), gameID, true); err != nil {
					slog.Debug("re-focus the panel", "err", err)
				}
			}
		}
	})
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
func (f *Flow) startPinnedHover(seq uint64, gameID *uint64) {
	if gameID == nil || !f.st.pinned.Load() {
		return
	}
	f.goSafe("pinned hover", func() {
		st := f.st
		var insideSince time.Time
		for f.sleep(hoverPoll) && f.shown(seq) && st.pinned.Load() {
			w := f.d.Overlay()
			cx, cy, ok := f.d.CursorPosition()
			if !ok {
				continue
			}
			x, y, wd, ht := w.Bounds()
			if !pointInRect(cx, cy, float64(x), float64(y), float64(wd), float64(ht)) {
				insideSince = time.Time{}
				continue
			}
			if insideSince.IsZero() {
				insideSince = time.Now()
			}
			if time.Since(insideSince) >= hoverFocusDelay && !w.IsFocused() {
				if err := f.d.BringToFront(w, gameID, true); err != nil {
					st.RecordError("focus pinned panel", err)
				}
				// Once per hover: leave and come back to focus it again.
				insideSince = time.Now().Add(time.Hour)
			}
		}
	})
}

// setOverlayPinned switches between covering the game (unpinned) and a card-sized window
// (pinned) in place.
func (f *Flow) setOverlayPinned(pinned bool) {
	st := f.st
	f.setPinned(pinned)
	if st.overlayVisible.Load() {
		w := f.d.Overlay()
		card, _ := st.cardPosition()
		f.layoutOverlay(w, card)
		if err := f.d.BringToFront(w, f.gameID(), true); err != nil {
			st.RecordError("overlay focus", err)
		}
	}
	f.emitOverlayState()
}

// resizeOverlay fits the panel window to its content (height in logical px, width in CSS px,
// measured by the web view). The height is clamped to the monitor and the window kept on
// screen.
func (f *Flow) resizeOverlay(width, height float64) {
	st := f.st
	w := f.d.Overlay()
	scale := w.ScaleFactor()
	x, y, _, _ := w.Bounds()
	monitor, haveMonitor := f.d.MonitorAt(float64(x), float64(y))
	screenW, screenH := math.Inf(1), math.Inf(1)
	if haveMonitor {
		screenW, screenH = float64(monitor.Width)/scale, float64(monitor.Height)/scale
	}
	zoom := st.Settings().Overlay.Scale
	h := fitOverlayHeight(height*zoom, screenH)
	// The card is 420 CSS px wide, or wider with results beside the filters; never wider than
	// the screen.
	maxCSS := 4000.0
	if haveMonitor {
		maxCSS = screenW / zoom
	}
	if math.IsNaN(width) {
		width = cardWidth
	}
	cssWidth := min(max(width, cardWidth), max(maxCSS, cardWidth))
	logicalWidth := cssWidth * zoom
	st.setCardSize(h, cssWidth)

	// Covering the game: the window stays game-sized. A card the user has not moved is pulled
	// up when its content grows past the bottom of the game.
	if st.layout().Cover {
		if g, ok := f.gameWindow(); ok && !st.cardMoved.Load() {
			card, _ := st.cardPosition()
			b := cardBounds(g.Rect)
			if ny := keepOnScreen(card.Y, physical(h, scale), b.Y, b.Height); ny != card.Y {
				card.Y = ny
				st.setCardPosition(card)
				f.setLayout(coverLayout(card, g.Rect, scale, zoom))
			}
		}
		return
	}
	w.SetLogicalSize(logicalWidth, h)
	f.roundCard(w, logicalWidth, h, zoom)
	if haveMonitor {
		nx := keepOnScreen(x, physical(logicalWidth, scale), monitor.X, monitor.Width)
		ny := keepOnScreen(y, physical(h, scale), monitor.Y, monitor.Height)
		if nx != x || ny != y {
			w.SetPosition(nx, ny)
			st.setCardPosition(point{nx, ny})
		}
	}
}

// dragCard moves the price card while the user drags its header. The web view only reports
// the phase: the cursor is read here (web view screen coordinates go stale while the window
// moves under the pointer). A card-sized window moves itself; over the game the card moves
// inside the covering window. DragEnd remembers the spot for `Position: Last`.
func (f *Flow) dragCard(phase ipc.DragPhase) error {
	st := f.st
	w := f.d.Overlay()
	cx, cy, ok := f.d.CursorPosition()
	if !ok {
		return ipc.NewError(ipc.KindInternal, "cursor position unavailable")
	}
	if phase == ipc.DragStart {
		card, _ := st.cardPosition()
		st.beginDrag(dragOrigin{cursorX: cx, cursorY: cy, card: card})
		st.cardMoved.Store(true)
		return nil
	}
	origin, ok := st.dragStart()
	if !ok {
		return nil
	}
	card := point{
		X: origin.card.X + int32(math.Round(cx-origin.cursorX)),
		Y: origin.card.Y + int32(math.Round(cy-origin.cursorY)),
	}
	cover := st.layout().Cover
	g, haveGame := f.gameWindow()
	zoom := st.Settings().Overlay.Scale
	// Over the game, keep the card's left edge on the game and at least 120 CSS px of it
	// visible (the web view applies the same rule).
	if cover && haveGame {
		px := w.ScaleFactor() * zoom
		width := int32(math.Round(st.cardWidthCSS() * px))
		visible := int32(math.Round(120 * px))
		b := cardBounds(g.Rect)
		card.X = min(max(card.X, b.X), max(b.X+b.Width-width, b.X))
		card.Y = min(max(card.Y, b.Y), max(b.Y+b.Height-visible, b.Y))
	}
	st.setCardPosition(card)
	switch {
	case cover && haveGame:
		f.setLayout(coverLayout(card, g.Rect, w.ScaleFactor(), zoom))
	case !cover:
		w.SetPosition(card.X, card.Y)
	}
	if phase == ipc.DragEnd {
		st.endDrag()
		st.setDroppedPosition(card)
	}
	return nil
}
