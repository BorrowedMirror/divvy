package app

import (
	"fmt"
	"math"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/autostart"
	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
)

// settingsWidth and settingsHeight are the settings window's size (logical px) when it is not
// bound to the game.
const settingsWidth, settingsHeight = 900.0, 680.0

// ShowSettings shows the settings window on route (`settings`, `diagnostics`, `onboarding`).
//
// With a tracked game window the settings become an overlay that covers the game exactly
// (undecorated, transparent backdrop, card centred, kept above the game); otherwise it is a
// normal decorated window.
func (f *Flow) ShowSettings(route string) {
	st := f.st
	w := f.d.Settings()
	g, overlay := f.gameWindow()
	wasOverlay := st.settingsOverlay.Swap(overlay)
	if overlay {
		w.SetFrameless(true)
		w.SetSkipTaskbar(true)
		coverGame(w, g.Rect)
	} else if wasOverlay {
		w.SetAlwaysOnTop(false)
		w.SetSkipTaskbar(false)
		w.SetFrameless(false)
		w.SetLogicalSize(settingsWidth, settingsHeight)
		w.Center()
	}
	overlayFlag := "0"
	if overlay {
		overlayFlag = "1"
	}
	w.ExecJS(fmt.Sprintf("document.documentElement.dataset.overlay = '%s'; window.location.hash = '#/%s'", overlayFlag, route))
	w.Show()
	if !overlay {
		w.Focus()
		return
	}
	if err := f.d.BringToFront(w, new(g.NativeID), true); err != nil {
		st.RecordError("settings raise", err)
	}
}

// hideSettings hides the settings window (Esc / backdrop click in overlay mode, close button).
func (f *Flow) hideSettings() { f.d.Settings().Hide() }

// showSettingsTab opens settings on one tab (`waystones`, `hotkeys`, ...): lowercase ASCII
// letters only, so the tab cannot change the route.
func (f *Flow) showSettingsTab(tab string) error {
	if tab == "" || len(tab) > 20 || strings.IndexFunc(tab, func(r rune) bool { return r < 'a' || r > 'z' }) >= 0 {
		return ipc.NewError(ipc.KindInternal, "unknown settings tab")
	}
	f.ShowSettings("settings?tab=" + tab)
	return nil
}

// applySettings validates, registers hotkeys (rolling back to the previous chords on
// failure), persists, swaps sources and settings, and broadcasts. It can block for seconds
// (portal registration): call it off the UI thread. Serialised by State.applyMu.
func (f *Flow) applySettings(next config.Settings) (config.Settings, error) {
	st := f.st
	if err := config.Validate(&next); err != nil {
		return config.Settings{}, ipc.From(err)
	}
	st.applyMu.Lock()
	defer st.applyMu.Unlock()
	previous := st.Settings()
	hotkeysChanged := !hotkeysEqual(&previous, &next)
	rollback := func(what string, err error) error {
		if hotkeysChanged {
			if re := f.registerHotkeys(&previous); re != nil {
				st.RecordError("hotkey rollback failed", re)
			}
		}
		st.RecordError(what, err)
		return err
	}
	if hotkeysChanged {
		if err := f.registerHotkeys(&next); err != nil {
			return config.Settings{}, rollback("register hotkeys", err)
		}
	}
	sources, err := st.priceSources().updated(&next)
	if err != nil {
		return config.Settings{}, rollback("price sources", ipc.Errorf(ipc.KindConfig, "%w", err))
	}
	if previous.UI.Autostart != next.UI.Autostart {
		if err := autostart.Set(next.UI.Autostart); err != nil {
			return config.Settings{}, rollback("start at login", ipc.Errorf(ipc.KindPlatform, "start at login: %w", err))
		}
	}
	if st.paths != nil {
		if err := config.Save(*st.paths, &next); err != nil {
			return config.Settings{}, rollback("save settings", ipc.From(err))
		}
	}
	st.setPriceSources(sources)
	st.setSettings(next.Clone())
	if previous.UI.Language != next.UI.Language {
		f.d.RelabelTray(next.UI.Language)
	}
	if math.Abs(previous.Overlay.Scale-next.Overlay.Scale) > 1e-12 {
		f.ApplyPanelScale(next.Overlay.Scale)
	}
	f.emit(ipc.EventSettingsChanged, ipc.SettingsChanged{Settings: next.Clone()})
	return next, nil
}
