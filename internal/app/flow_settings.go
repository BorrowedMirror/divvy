package app

import (
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// HotkeyBindings is every hotkey from settings, chat commands and stash searches included.
// Cleared ("Not set") hotkeys and empty commands are not bound.
func HotkeyBindings(s *config.Settings) []platform.Binding {
	all := []platform.Binding{
		{Action: platform.Action(platform.PriceCheck), Chord: s.Hotkeys.PriceCheck},
		{Action: platform.Action(platform.PriceCheckPinned), Chord: s.Hotkeys.PriceCheckPinned},
		{Action: platform.Action(platform.OpenSettings), Chord: s.Hotkeys.OpenSettings},
		{Action: platform.Action(platform.OpenTrade), Chord: s.Hotkeys.OpenTrade},
	}
	for i, c := range s.Chat.Commands {
		if strings.TrimSpace(c.Text) != "" {
			all = append(all, platform.Binding{Action: platform.ChatAction(uint8(i)), Chord: c.Chord})
		}
	}
	for i, c := range s.Stash.Searches {
		if strings.TrimSpace(c.Text) != "" {
			all = append(all, platform.Binding{Action: platform.StashSearchAction(uint8(i)), Chord: c.Chord})
		}
	}
	bound := all[:0]
	for _, b := range all {
		if !b.Chord.IsUnbound() {
			bound = append(bound, b)
		}
	}
	return bound
}

// RegisterHotkeys (re)registers every hotkey from settings. When hotkeys follow the game's
// focus and the game is not focused, they are released again right away: registering still
// checks the chords.
func (f *Flow) RegisterHotkeys(s *config.Settings) error {
	p := f.st.Platform
	if p == nil || !p.Capabilities.GlobalHotkeys {
		return nil
	}
	if err := p.Hotkeys.UnregisterAll(); err != nil {
		return ipc.From(err)
	}
	// One call so the Wayland portal binds (and confirms) all shortcuts together.
	if err := p.Hotkeys.RegisterAll(HotkeyBindings(s)); err != nil {
		return ipc.From(err)
	}
	if f.st.FollowFocus.Load() && !f.hotkeysWanted() {
		if err := p.Hotkeys.UnregisterAll(); err != nil {
			return ipc.From(err)
		}
	}
	return nil
}

// hotkeysWanted reports whether the hotkeys should be grabbed: while the game or one of RKL's
// own windows (the panel, settings) has focus, so Ctrl+Alt+D and the settings hotkey still
// close what they opened.
func (f *Flow) hotkeysWanted() bool {
	if f.st.Platform.WindowTracker.IsGameFocused() {
		return true
	}
	return f.d.Overlay().IsFocused() || f.d.Settings().IsFocused()
}

// SyncHotkeysToFocus grabs the hotkeys while the game or an RKL window has focus and releases
// them otherwise, so the same keys still work in other apps. The shell also calls it when an
// RKL window gains or loses focus. Only once the game's focus is tracked (not on
// Wayland, where every portal registration asks the user); until then the hotkeys stay
// grabbed everywhere.
func (f *Flow) SyncHotkeysToFocus() {
	st := f.st
	p := st.Platform
	if p == nil || !st.FollowFocus.Load() || !p.Capabilities.GlobalHotkeys {
		return
	}
	st.ApplyLock.Lock()
	defer st.ApplyLock.Unlock()
	focused := f.hotkeysWanted()
	slog.Debug("focus changed", "grab_hotkeys", focused)
	var err error
	if focused {
		settings := st.Settings()
		err = f.RegisterHotkeys(&settings)
	} else {
		err = p.Hotkeys.UnregisterAll()
	}
	if err != nil {
		st.RecordError(fmt.Sprintf("hotkeys: %v", err))
	}
}

// ApplySettings validates, registers hotkeys (rolling back to the previous chords on
// failure), persists, swaps sources and settings, and broadcasts. It can block for seconds
// (portal registration): call it off the UI thread. Serialised by State.ApplyLock.
func (f *Flow) ApplySettings(next config.Settings) (config.Settings, error) {
	st := f.st
	if err := config.Validate(&next); err != nil {
		return config.Settings{}, ipc.From(err)
	}
	st.ApplyLock.Lock()
	defer st.ApplyLock.Unlock()
	previous := st.Settings()
	hotkeysChanged := !hotkeysEqual(&previous, &next)
	rollback := func(e error) error {
		if hotkeysChanged {
			if re := f.RegisterHotkeys(&previous); re != nil {
				st.RecordError(fmt.Sprintf("hotkey rollback failed: %v", re))
			}
		}
		st.RecordError(e)
		return e
	}
	if hotkeysChanged {
		if err := f.RegisterHotkeys(&next); err != nil {
			return config.Settings{}, rollback(err)
		}
	}
	sources, err := st.Sources().Updated(&next)
	if err != nil {
		return config.Settings{}, rollback(ipc.NewError("config", err.Error()))
	}
	if previous.UI.Autostart != next.UI.Autostart {
		if err := SetAutostart(next.UI.Autostart); err != nil {
			return config.Settings{}, rollback(ipc.Errorf("platform", "start at login: %v", err))
		}
	}
	if st.Paths != nil {
		if err := config.Save(*st.Paths, &next); err != nil {
			return config.Settings{}, rollback(ipc.From(err))
		}
	}
	if sources != nil {
		st.SetSources(sources)
	}
	st.SetSettings(next.Clone())
	if previous.UI.Language != next.UI.Language {
		f.d.RelabelTray(next.UI.Language)
	}
	if math.Abs(previous.Overlay.Scale-next.Overlay.Scale) > 1e-12 {
		f.ApplyPanelScale(next.Overlay.Scale)
	}
	f.emit(ipc.EventSettingsChanged, ipc.SettingsChanged{Settings: next.Clone()})
	return next, nil
}

// hotkeysEqual compares the hotkey settings and every chord that registration depends on.
func hotkeysEqual(a, b *config.Settings) bool {
	ab, bb := HotkeyBindings(a), HotkeyBindings(b)
	if a.Hotkeys.AdvancedCopy != b.Hotkeys.AdvancedCopy || len(ab) != len(bb) {
		return false
	}
	for i := range ab {
		if ab[i].Action != bb[i].Action || !ab[i].Chord.Equal(bb[i].Chord) {
			return false
		}
	}
	return true
}

// RunHotkeyLoop forwards fired hotkeys to the flows until the event stream closes.
func (f *Flow) RunHotkeyLoop() {
	p := f.st.Platform
	if p == nil {
		return
	}
	events, err := p.Hotkeys.Events()
	if err != nil {
		f.st.RecordError(fmt.Sprintf("hotkey events unavailable: %v", err))
		return
	}
	go func() {
		defer f.recoverPanic("hotkey loop")
		for ev := range events {
			f.onHotkey(ev.Action)
		}
	}()
}

func (f *Flow) onHotkey(action platform.HotkeyAction) {
	st := f.st
	effect := HotkeyEffectFor(action, st.OverlayVisible.Load(), f.d.Settings().IsVisible())
	switch effect.Kind {
	case EffectPriceCheck:
		f.Trigger(effect.Pinned)
	case EffectHideOverlay:
		_ = f.HideOverlay()
	case EffectShowSettings:
		_ = f.ShowSettings("settings")
	case EffectHideSettings:
		_ = f.HideSettings()
	case EffectRunChat:
		settings := st.Settings()
		if int(effect.Index) < len(settings.Chat.Commands) {
			c := settings.Chat.Commands[effect.Index]
			f.RunMacro(func() ([]MacroStep, error) { return f.chatMacro(c.Text, c.Send) })
		}
	case EffectRunStash:
		settings := st.Settings()
		if int(effect.Index) < len(settings.Stash.Searches) {
			text := settings.Stash.Searches[effect.Index].Text
			f.RunMacro(func() ([]MacroStep, error) { return StashSteps(text), nil })
		}
	case EffectOpenTrade:
		st.mu.Lock()
		var url *string
		if st.lastPrice != nil {
			url = st.lastPrice.TradeURL
		}
		st.mu.Unlock()
		if url == nil {
			st.RecordError("open on trade site: no price check with a trade search yet")
			return
		}
		if err := f.OpenTrade(*url); err != nil {
			st.RecordError(err)
		}
	}
}

// RunInputLoop uses the passive hook (where supported): Esc closes an unpinned panel even
// when it never took keyboard focus from the game, and Ctrl + wheel over the game switches
// stash tabs (one arrow key per wheel step).
func (f *Flow) RunInputLoop() {
	st := f.st
	p := st.Platform
	if p == nil || !p.Capabilities.PassiveHook {
		return
	}
	events, err := p.InputListener.Start()
	if err != nil {
		st.RecordError(fmt.Sprintf("passive input hook unavailable: %v", err))
		return
	}
	go func() {
		defer f.recoverPanic("input loop")
		ctrl := false
		for ev := range events {
			switch {
			case ev.Type == platform.KeyDown && IsCtrlKey(ev.Key):
				ctrl = true
			case ev.Type == platform.KeyUp && IsCtrlKey(ev.Key):
				ctrl = false
			case ev.Type == platform.KeyDown && IsCloseKey(ev.Key):
				if st.OverlayVisible.Load() && !st.IsPinned() {
					_ = f.HideOverlay()
				}
			case ev.Type == platform.Wheel && ctrl:
				key, ok := WheelTabKey(ev.Delta)
				settings := st.Settings()
				if !ok || !settings.Stash.CtrlScrollTabs || !p.WindowTracker.IsGameFocused() {
					continue
				}
				// Ctrl+Arrow with the user's Ctrl left down: releasing it would hide the user's
				// own release from the hook.
				if err := p.InputSimulator.SendChord(platform.NewChord(key, platform.Ctrl)); err != nil {
					st.RecordError(fmt.Sprintf("stash tab scroll: %v", err))
				}
			}
		}
	}()
}

// RunGameFollower keeps the settings overlay glued to the game window while it moves or
// resizes, hides it when the game window goes away, grabs the hotkeys only while the game is
// focused, and keeps a pinned panel above a fullscreen game that is clicked back into.
func (f *Flow) RunGameFollower() {
	st := f.st
	p := st.Platform
	if p == nil || !p.Capabilities.WindowTracking || st.Backend == platform.BackendWayland {
		return
	}
	events, err := p.WindowTracker.Subscribe()
	if err != nil {
		st.RecordError(fmt.Sprintf("game window tracking unavailable: %v", err))
		return
	}
	st.FollowFocus.Store(true)
	go func() {
		defer f.recoverPanic("game follower")
		// Startup registration may have finished before following focus was switched on.
		f.SyncHotkeysToFocus()
		for ev := range events {
			switch ev.Type {
			case platform.WindowFound:
				slog.Info("game window found", "rect", ev.Info.Rect)
			case platform.WindowLost:
				slog.Info("game window lost")
			}
			if ev.Type != platform.WindowMoved {
				f.SyncHotkeysToFocus()
			}
			if ev.Type == platform.WindowFocusChanged && ev.Focused && st.IsPinned() && st.OverlayVisible.Load() {
				// Clicking back into a fullscreen game makes WMs such as openbox raise it above
				// "always on top" windows: close the pinned panel if the user asked for that,
				// else keep it stacked above the game without taking keyboard focus back.
				if st.Settings().Overlay.ClosePinnedOnGameFocus {
					_ = f.HideOverlay()
					continue
				}
				if err := f.d.BringToFront(f.d.Overlay(), f.gameID(), false); err != nil {
					st.RecordError(fmt.Sprintf("keep pinned panel on top: %v", err))
				}
			}
			if !st.SettingsOverlay.Load() || !f.d.Settings().IsVisible() {
				continue
			}
			switch ev.Type {
			case platform.WindowFound:
				f.coverGame(f.d.Settings(), ev.Info.Rect)
			case platform.WindowMoved:
				f.coverGame(f.d.Settings(), ev.Rect)
			case platform.WindowLost:
				f.d.Settings().Hide()
			}
		}
	}()
}
