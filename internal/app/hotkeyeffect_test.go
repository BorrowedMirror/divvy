package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestHotkeysToggleTheirWindow(t *testing.T) {
	a := platform.Action
	testCases := []struct {
		name         string
		action       platform.HotkeyAction
		overlayOpen  bool
		settingsOpen bool
		expected     hotkeyEffect
	}{
		{
			"PriceCheckOpensOverlay",
			a(platform.PriceCheck),
			false,
			false,
			hotkeyEffect{kind: effectPriceCheck, pinned: false},
		},
		{
			"PinnedPriceCheckOpensPinnedOverlay",
			a(platform.PriceCheckPinned),
			false,
			true,
			hotkeyEffect{kind: effectPriceCheck, pinned: true},
		},
		{
			"PriceCheckHidesOpenOverlay",
			a(platform.PriceCheck),
			true,
			false,
			hotkeyEffect{kind: effectHideOverlay},
		},
		{
			"PinnedPriceCheckHidesOpenOverlay",
			a(platform.PriceCheckPinned),
			true,
			false,
			hotkeyEffect{kind: effectHideOverlay},
		},
		{
			"OpenSettingsShowsSettings",
			a(platform.OpenSettings),
			true,
			false,
			hotkeyEffect{kind: effectShowSettings},
		},
		{
			"OpenSettingsHidesOpenSettings",
			a(platform.OpenSettings),
			false,
			true,
			hotkeyEffect{kind: effectHideSettings},
		},
		{
			"OpenTradeOpensTrade",
			a(platform.OpenTrade),
			true,
			true,
			hotkeyEffect{kind: effectOpenTrade},
		},
		{
			"ChatActionRunsChat",
			platform.ChatAction(2),
			true,
			true,
			hotkeyEffect{kind: effectRunChat, index: 2},
		},
		{
			"StashSearchActionRunsStash",
			platform.StashSearchAction(0),
			false,
			false,
			hotkeyEffect{kind: effectRunStash, index: 0},
		},
		{
			"UnknownActionDoesNothing",
			a("bogus"),
			false,
			false,
			hotkeyEffect{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, hotkeyEffectFor(tc.action, tc.overlayOpen, tc.settingsOpen))
		})
	}
}
