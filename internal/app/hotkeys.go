package app

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// hotkeyBindings is every hotkey from settings, chat commands and stash searches included.
// Cleared ("Not set") hotkeys and empty commands are not bound.
func hotkeyBindings(s *config.Settings) []platform.Binding {
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
	return slices.DeleteFunc(all, func(b platform.Binding) bool { return b.Chord.IsUnbound() })
}

// hotkeysEqual compares the hotkey settings and every chord that registration depends on.
func hotkeysEqual(a, b *config.Settings) bool {
	return a.Hotkeys.AdvancedCopy == b.Hotkeys.AdvancedCopy &&
		slices.EqualFunc(hotkeyBindings(a), hotkeyBindings(b), func(x, y platform.Binding) bool {
			return x.Action == y.Action && x.Chord.Equal(y.Chord)
		})
}

// RegisterSettingsHotkeys registers every hotkey from the current settings (startup).
func (f *Flow) RegisterSettingsHotkeys() error {
	f.st.applyMu.Lock()
	defer f.st.applyMu.Unlock()
	settings := f.st.Settings()
	return f.registerHotkeys(&settings)
}

// registerHotkeys (re)registers every hotkey from s. When hotkeys follow the game's focus and
// the game is not focused, they are released again right away: registering still checks the
// chords. The caller holds State.applyMu.
func (f *Flow) registerHotkeys(s *config.Settings) error {
	p := f.st.platform
	if p == nil || !p.Capabilities.GlobalHotkeys {
		return nil
	}
	if err := p.Hotkeys.UnregisterAll(); err != nil {
		return ipc.From(err)
	}
	// One call so the Wayland portal binds (and confirms) all shortcuts together.
	if err := p.Hotkeys.RegisterAll(hotkeyBindings(s)); err != nil {
		return ipc.From(err)
	}
	if f.st.followFocus.Load() && !f.hotkeysWanted() {
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
	if f.st.platform.WindowTracker.IsGameFocused() {
		return true
	}
	return f.d.Overlay().IsFocused() || f.d.Settings().IsFocused()
}

// focusRecheck is how long after releasing the hotkeys, with an RKL window open, they are
// checked again.
const focusRecheck = 250 * time.Millisecond

// recheckHotkeys grabs the hotkeys again if an RKL window got focus after they were released.
// It never schedules another check, so the two cannot loop.
func (f *Flow) recheckHotkeys() {
	st := f.st
	if st.platform == nil || !st.followFocus.Load() || !f.hotkeysWanted() {
		return
	}
	st.applyMu.Lock()
	defer st.applyMu.Unlock()
	if !f.hotkeysWanted() {
		return
	}
	slog.Debug("focus recheck", "grab_hotkeys", true)
	settings := st.Settings()
	if err := f.registerHotkeys(&settings); err != nil {
		st.RecordError("hotkeys", err)
	}
}

// SyncHotkeysToFocus grabs the hotkeys while the game or an RKL window has focus and releases
// them otherwise, so the same keys still work in other apps. The shell also calls it when an
// RKL window gains or loses focus. Only once the game's focus is tracked (not on
// Wayland, where every portal registration asks the user); until then the hotkeys stay
// grabbed everywhere.
func (f *Flow) SyncHotkeysToFocus() {
	st := f.st
	p := st.platform
	if p == nil || !st.followFocus.Load() || !p.Capabilities.GlobalHotkeys {
		return
	}
	st.applyMu.Lock()
	defer st.applyMu.Unlock()
	focused := f.hotkeysWanted()
	slog.Debug("focus changed", "grab_hotkeys", focused)
	if !focused && (st.overlayVisible.Load() || f.d.Settings().IsVisible()) {
		// The game lost focus to an RKL window that the window manager may not have activated
		// yet (its own focus event can come later, or not at all); look again shortly.
		f.goSafe("hotkey recheck", func() {
			if f.sleep(focusRecheck) {
				f.recheckHotkeys()
			}
		})
	}
	var err error
	if focused {
		settings := st.Settings()
		err = f.registerHotkeys(&settings)
	} else {
		err = p.Hotkeys.UnregisterAll()
	}
	if err != nil {
		st.RecordError("hotkeys", err)
	}
}

// RunHotkeyLoop forwards fired hotkeys to the flows until the event stream closes or ctx is
// done.
func (f *Flow) RunHotkeyLoop(ctx context.Context) {
	p := f.st.platform
	if p == nil {
		return
	}
	events, err := p.Hotkeys.Events()
	if err != nil {
		f.st.RecordError("hotkey events unavailable", err)
		return
	}
	f.goSafe("hotkey loop", func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				f.onHotkey(ev.Action)
			}
		}
	})
}

func (f *Flow) onHotkey(action platform.HotkeyAction) {
	st := f.st
	effect := hotkeyEffectFor(action, st.overlayVisible.Load(), f.d.Settings().IsVisible())
	switch effect.kind {
	case effectNone:
	case effectPriceCheck:
		f.Trigger(effect.pinned)
	case effectHideOverlay:
		f.hideOverlay()
	case effectShowSettings:
		f.ShowSettings("settings")
	case effectHideSettings:
		f.hideSettings()
	case effectRunChat:
		settings := st.Settings()
		if int(effect.index) < len(settings.Chat.Commands) {
			c := settings.Chat.Commands[effect.index]
			f.runMacro(func() ([]macroStep, error) { return f.chatMacro(c.Text, c.Send) })
		}
	case effectRunStash:
		settings := st.Settings()
		if int(effect.index) < len(settings.Stash.Searches) {
			text := settings.Stash.Searches[effect.index].Text
			f.runMacro(func() ([]macroStep, error) { return stashSteps(text), nil })
		}
	case effectOpenTrade:
		u, ok := st.lastTradeURL()
		if !ok {
			st.RecordError("open on trade site: no price check with a trade search yet", nil)
			return
		}
		if err := f.openTrade(u); err != nil {
			st.RecordError("open on trade site", err)
		}
	}
}
