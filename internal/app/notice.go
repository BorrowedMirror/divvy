package app

import (
	"math"
	"time"

	"github.com/BorrowedMirror/divvy/internal/ipc"
	"github.com/BorrowedMirror/divvy/internal/platform"
)

// The ready notice: a small card shown over the game window for a few seconds when Divvy finds
// it, so the user knows Divvy is running. It never takes focus or mouse input.

const (
	// noticeWidth and noticeHeight are the notice window's size in logical px.
	noticeWidth, noticeHeight = 300.0, 64.0
	// noticeDuration is how long the notice stays up, its fade included.
	noticeDuration = 4 * time.Second
	// noticeHeightFraction is where the notice's centre sits, as a fraction of the game
	// window's height from its top: below the middle, clear of the character and menus.
	noticeHeightFraction = 2.0 / 3.0
	// noticeRadius is the notice card's corner radius in logical px (web/src/index.css).
	noticeRadius = 10.0
)

// noticeOrigin is the top-left corner of a width x height notice over game: centred
// horizontally, its centre at noticeHeightFraction of the game's height.
func noticeOrigin(game platform.Rect, width, height float64) (x, y float64) {
	x = float64(game.X) + (float64(game.Width)-width)/2
	y = float64(game.Y) + float64(game.Height)*noticeHeightFraction - height/2
	return x, y
}

// gameFound shows the ready notice over a new game window (tracker coordinates: physical px,
// macOS points) unless the user turned it off, and hides it after noticeDuration.
func (f *Flow) gameFound(game platform.Rect) {
	if !f.st.Settings().Overlay.ReadyNotice || game.Width == 0 || game.Height == 0 {
		return
	}
	gen := f.noticeGen.Add(1)
	w := f.d.Notice()
	f.placeNotice(w, game)
	f.emit(ipc.EventReadyNotice, ipc.ReadyNotice{DurationMS: uint32(noticeDuration.Milliseconds())})
	f.d.ShowInactive(w)
	// Stacked above a fullscreen game like the pinned panel, without taking its focus.
	if err := f.d.BringToFront(w, f.gameID(), false); err != nil {
		f.st.RecordError("ready notice", err)
	}
	f.goSafe("ready notice", func() {
		if f.sleep(noticeDuration) && f.noticeGen.Load() == gen {
			w.Hide()
		}
	})
}

// gameMoved keeps a visible notice over the game window while it moves or resizes (a game
// that is starting up often moves its window to its monitor and goes fullscreen).
func (f *Flow) gameMoved(game platform.Rect) {
	if w := f.d.Notice(); w.IsVisible() && game.Width > 0 && game.Height > 0 {
		f.placeNotice(w, game)
	}
}

// gameLost hides the notice and the price panel, pinned or not: the game window went away, so
// there is nothing left for them to sit over.
func (f *Flow) gameLost() {
	f.dismissNotice()
	if f.st.overlayVisible.Load() {
		f.hideOverlay()
	}
	for _, w := range []Window{f.d.Overlay(), f.d.Notice(), f.d.Settings()} {
		f.d.DetachFromGame(w)
	}
}

// dismissNotice hides the notice now: the game went away, or the user opened the price panel
// or settings, which the notice must not cover.
func (f *Flow) dismissNotice() {
	f.noticeGen.Add(1)
	if w := f.d.Notice(); w.IsVisible() {
		w.Hide()
	}
}

// placeNotice moves and sizes the notice window over game. Its corners are cut with a window
// shape so they stay round without a compositor.
func (f *Flow) placeNotice(w Window, game platform.Rect) {
	if macPoints {
		x, y := noticeOrigin(game, noticeWidth, noticeHeight)
		w.SetLogicalBounds(x, y, noticeWidth, noticeHeight)
		return
	}
	scale := w.ScaleFactor()
	width, height := physical(noticeWidth, scale), physical(noticeHeight, scale)
	x, y := noticeOrigin(game, float64(width), float64(height))
	w.SetSize(width, height)
	w.SetPosition(int32(math.Round(x)), int32(math.Round(y)))
	f.d.RoundWindow(w, width, height, physical(noticeRadius, scale))
}
