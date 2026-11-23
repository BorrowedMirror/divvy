package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestHotkeysToggleTheirWindow(t *testing.T) {
	a := platform.Action
	for _, c := range []struct {
		action                    platform.HotkeyAction
		overlayOpen, settingsOpen bool
		want                      hotkeyEffect
	}{
		{a(platform.PriceCheck), false, false, hotkeyEffect{kind: effectPriceCheck, pinned: false}},
		{a(platform.PriceCheckPinned), false, true, hotkeyEffect{kind: effectPriceCheck, pinned: true}},
		{a(platform.PriceCheck), true, false, hotkeyEffect{kind: effectHideOverlay}},
		{a(platform.PriceCheckPinned), true, false, hotkeyEffect{kind: effectHideOverlay}},
		{a(platform.OpenSettings), true, false, hotkeyEffect{kind: effectShowSettings}},
		{a(platform.OpenSettings), false, true, hotkeyEffect{kind: effectHideSettings}},
		{a(platform.OpenTrade), true, true, hotkeyEffect{kind: effectOpenTrade}},
		{platform.ChatAction(2), true, true, hotkeyEffect{kind: effectRunChat, index: 2}},
		{platform.StashSearchAction(0), false, false, hotkeyEffect{kind: effectRunStash, index: 0}},
		{a("bogus"), false, false, hotkeyEffect{}},
	} {
		assert.Equal(t, c.want, hotkeyEffectFor(c.action, c.overlayOpen, c.settingsOpen), "%+v", c)
	}
}
