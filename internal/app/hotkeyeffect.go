package app

import "github.com/BorrowedMirror/rust-king-ledger/internal/platform"

// hotkeyEffectKind says which [hotkeyEffect] fields are set.
type hotkeyEffectKind int

// hotkeyEffectKind values. The zero value is not an effect.
const (
	effectNone hotkeyEffectKind = iota
	// effectPriceCheck runs a price check (hotkeyEffect.pinned).
	effectPriceCheck
	effectHideOverlay
	effectShowSettings
	effectHideSettings
	effectOpenTrade
	// effectRunChat runs chat command hotkeyEffect.index.
	effectRunChat
	// effectRunStash runs stash search hotkeyEffect.index.
	effectRunStash
)

// hotkeyEffect is what a global hotkey press does. Every hotkey toggles: pressing it while its
// window is open closes it (like Esc), otherwise it opens it.
type hotkeyEffect struct {
	kind   hotkeyEffectKind
	pinned bool  // effectPriceCheck
	index  uint8 // effectRunChat, effectRunStash
}

// hotkeyEffectFor maps a hotkey press to its effect given which windows are open. An unknown
// action kind has no effect.
func hotkeyEffectFor(action platform.HotkeyAction, overlayOpen, settingsOpen bool) hotkeyEffect {
	switch action.Kind {
	case platform.PriceCheck, platform.PriceCheckPinned:
		if overlayOpen {
			return hotkeyEffect{kind: effectHideOverlay}
		}
		return hotkeyEffect{kind: effectPriceCheck, pinned: action.Kind == platform.PriceCheckPinned}
	case platform.OpenSettings:
		if settingsOpen {
			return hotkeyEffect{kind: effectHideSettings}
		}
		return hotkeyEffect{kind: effectShowSettings}
	case platform.OpenTrade:
		return hotkeyEffect{kind: effectOpenTrade}
	case platform.Chat:
		return hotkeyEffect{kind: effectRunChat, index: action.Index}
	case platform.StashSearch:
		return hotkeyEffect{kind: effectRunStash, index: action.Index}
	}
	return hotkeyEffect{}
}
