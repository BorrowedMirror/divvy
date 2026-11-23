package platform_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func accel(t *testing.T, chord string) string {
	t.Helper()
	c, err := platform.ParseChord(chord)
	require.NoError(t, err, chord)
	a, err := platform.Accelerator(c)
	require.NoError(t, err, chord)
	return a
}

// wailsCanonical runs s through Wails' own (unexported) accelerator parser, reached via
// MenuItem.SetAccelerator, and returns Wails' canonical form. ok is false when Wails rejects it.
func wailsCanonical(s string) (canonical string, ok bool) {
	defer func() {
		// An invalid accelerator makes SetAccelerator report through the global application,
		// which does not exist in tests and panics.
		if recover() != nil {
			canonical, ok = "", false
		}
	}()
	item := application.NewMenuItem("probe").SetAccelerator(s)
	got := item.GetAccelerator()
	return got, got != ""
}

func TestAcceleratorMapsLettersFunctionKeysAndModifiers(t *testing.T) {
	assert.Equal(t, "Ctrl+D", accel(t, "Ctrl+D"))
	assert.Equal(t, "Ctrl+Alt+Shift+Super+F5", accel(t, "Ctrl+Alt+Shift+Meta+F5"))
	assert.Equal(t, "Alt+1", accel(t, "Alt+1"))
	a, err := platform.Accelerator(platform.NewChord("Space"))
	require.NoError(t, err)
	assert.Equal(t, "Space", a)
}

func TestAcceleratorMetaAndSuperAreTheSameShortcut(t *testing.T) {
	// Meta maps to Wails' Super (Windows key, Command on macOS).
	ours, ok := wailsCanonical(accel(t, "Meta+D"))
	require.True(t, ok)
	theirs, ok := wailsCanonical("super+d")
	require.True(t, ok)
	assert.Equal(t, theirs, ours)
}

func TestAcceleratorMatchesWailsStringParser(t *testing.T) {
	testCases := []struct {
		name  string
		chord string
		wails string
	}{
		{
			"Letter",
			"Ctrl+Alt+D",
			"ctrl+alt+d",
		},
		{
			"FunctionKey",
			"Shift+F5",
			"shift+f5",
		},
		{
			"Space",
			"Ctrl+Space",
			"ctrl+space",
		},
		{
			"ArrowWithReorderedModifiers",
			"Alt+Ctrl+ArrowUp",
			"ctrl+alt+up",
		},
		{
			"PageDown",
			"Ctrl+PageDown",
			"ctrl+page down",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ours, ok := wailsCanonical(accel(t, tc.chord))
			require.True(t, ok)
			theirs, ok := wailsCanonical(tc.wails)
			require.True(t, ok)
			assert.Equal(t, theirs, ours)
		})
	}
}

func TestAcceleratorSameChordSameString(t *testing.T) {
	assert.Equal(t, accel(t, "Ctrl+D"), accel(t, "ctrl+d"))
	assert.Equal(t, accel(t, "Ctrl+Alt+D"), accel(t, "Alt+Ctrl+Ctrl+D"))
}

func TestAcceleratorRejectsUnknownAndModifierKeys(t *testing.T) {
	for _, key := range []string{
		"NotAKey", "Unidentified", "ControlLeft", "ControlRight", "AltLeft", "AltRight",
		"ShiftLeft", "ShiftRight", "MetaLeft", "MetaRight", "Fn", "FnLock", "d", "keyd", "", " ",
		// Real keys Wails cannot bind.
		"Numpad0", "NumpadAdd", "Insert", "CapsLock", "PrintScreen", "IntlBackslash", "F25",
	} {
		_, err := platform.Accelerator(platform.NewChord(key, platform.Ctrl))
		require.ErrorIs(t, err, platform.ErrInvalidChord, "%q", key)
	}
	_, err := platform.Accelerator(platform.NewChord("KeyD", platform.KeyModifier("hyper")))
	require.ErrorIs(t, err, platform.ErrInvalidChord)
	// Wails has no names for these either, so the probe does detect rejection.
	for _, s := range []string{"ctrl+numpad0", "ctrl+insert", "hyper+d"} {
		_, ok := wailsCanonical(s)
		assert.False(t, ok, s)
	}
}

// w3cCodes is a broad table of W3C KeyboardEvent.code names a user could bind.
func w3cCodes() []string {
	codes := []string{
		"Backquote", "Minus", "Equal", "BracketLeft", "BracketRight", "Backslash", "Semicolon",
		"Quote", "Comma", "Period", "Slash", "Backspace", "Tab", "Enter", "Escape", "Space",
		"PageUp", "PageDown", "End", "Home", "ArrowLeft", "ArrowUp", "ArrowRight", "ArrowDown",
		"Delete", "NumLock",
	}
	for c := 'A'; c <= 'Z'; c++ {
		codes = append(codes, "Key"+string(c))
	}
	for c := '0'; c <= '9'; c++ {
		codes = append(codes, "Digit"+string(c))
	}
	for i := 1; i <= 24; i++ {
		codes = append(codes, fmt.Sprintf("F%d", i))
	}
	return codes
}

func TestEveryAcceleratorIsAcceptedByWails(t *testing.T) {
	mods := [][]platform.KeyModifier{
		nil,
		{platform.Ctrl},
		{platform.Alt},
		{platform.Shift},
		{platform.Meta},
		{platform.Ctrl, platform.Alt, platform.Shift, platform.Meta},
	}
	for _, code := range w3cCodes() {
		for _, m := range mods {
			chord := platform.NewChord(code, m...)
			a, err := platform.Accelerator(chord)
			require.NoError(t, err, code)
			_, ok := wailsCanonical(a)
			assert.True(t, ok, "%s -> %q rejected by Wails", chord, a)
		}
	}
}
