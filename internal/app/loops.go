package app

import (
	"context"
	"log/slog"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// RunInputLoop uses the passive hook (where supported): Esc closes an unpinned panel even
// when it never took keyboard focus from the game, and Ctrl + wheel over the game switches
// stash tabs (one arrow key per wheel step). The hook runs until ctx is done.
func (f *Flow) RunInputLoop(ctx context.Context) {
	st := f.st
	p := st.platform
	if p == nil || !p.Capabilities.PassiveHook {
		return
	}
	events, err := p.InputListener.Start(ctx)
	if err != nil {
		st.RecordError("passive input hook unavailable", err)
		return
	}
	f.goSafe("input loop", func() {
		ctrl := false
		for ev := range events {
			switch {
			case ev.Type == platform.KeyDown && isCtrlKey(ev.Key):
				ctrl = true
			case ev.Type == platform.KeyUp && isCtrlKey(ev.Key):
				ctrl = false
			case ev.Type == platform.KeyDown && isCloseKey(ev.Key):
				if st.overlayVisible.Load() && !st.pinned.Load() {
					f.hideOverlay()
				}
			case ev.Type == platform.Wheel && ctrl:
				key, ok := wheelTabKey(ev.Delta)
				if !ok || !st.Settings().Stash.CtrlScrollTabs || !p.WindowTracker.IsGameFocused() {
					continue
				}
				// Ctrl+Arrow with the user's Ctrl left down: releasing it would hide the user's
				// own release from the hook.
				if err := p.InputSimulator.SendChord(platform.NewChord(key, platform.Ctrl)); err != nil {
					st.RecordError("stash tab scroll", err)
				}
			}
		}
	})
}

// RunGameFollower keeps the settings overlay glued to the game window while it moves or
// resizes, hides it when the game window goes away, shows the ready notice when a new game
// window appears, grabs the hotkeys only while the game is focused, and keeps a pinned
// panel above a fullscreen game that is clicked back into. It follows the game until ctx is
// done.
func (f *Flow) RunGameFollower(ctx context.Context) {
	st := f.st
	p := st.platform
	if p == nil || !p.Capabilities.WindowTracking || st.backend == platform.BackendWayland {
		return
	}
	events, err := p.WindowTracker.Subscribe(ctx)
	if err != nil {
		st.RecordError("game window tracking unavailable", err)
		return
	}
	st.followFocus.Store(true)
	f.goSafe("game follower", func() {
		// Startup registration may have finished before following focus was switched on.
		f.SyncHotkeysToFocus()
		for ev := range events {
			f.onWindowEvent(ev)
		}
	})
}

func (f *Flow) onWindowEvent(ev platform.WindowEvent) {
	st := f.st
	switch ev.Type {
	case platform.WindowFound:
		slog.Info("game window found", "rect", ev.Info.Rect)
		f.gameFound(ev.Info.Rect)
	case platform.WindowLost:
		slog.Info("game window lost")
		f.gameLost()
	case platform.WindowMoved:
		f.gameMoved(ev.Rect)
	case platform.WindowFocusChanged:
	}
	if ev.Type != platform.WindowMoved {
		f.SyncHotkeysToFocus()
	}
	if ev.Type == platform.WindowFocusChanged && ev.Focused && st.pinned.Load() && st.overlayVisible.Load() {
		// Clicking back into a fullscreen game makes WMs such as openbox raise it above
		// "always on top" windows: close the pinned panel if the user asked for that,
		// else keep it stacked above the game without taking keyboard focus back.
		if st.Settings().Overlay.ClosePinnedOnGameFocus {
			f.hideOverlay()
			return
		}
		if err := f.d.BringToFront(f.d.Overlay(), f.gameID(), false); err != nil {
			st.RecordError("keep pinned panel on top", err)
		}
	}
	if !st.settingsOverlay.Load() || !f.d.Settings().IsVisible() {
		return
	}
	switch ev.Type {
	case platform.WindowFound:
		coverGame(f.d.Settings(), ev.Info.Rect)
	case platform.WindowMoved:
		coverGame(f.d.Settings(), ev.Rect)
	case platform.WindowLost:
		f.d.Settings().Hide()
	case platform.WindowFocusChanged:
	}
}
