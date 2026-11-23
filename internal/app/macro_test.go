package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func key(k string, mods ...platform.KeyModifier) macroStep {
	return pressStep(platform.NewChord(k, mods...))
}

func assertSteps(t *testing.T, got []macroStep, err error, want ...macroStep) {
	t.Helper()
	require.NoError(t, err)
	require.Len(t, got, len(want), "got %+v", got)
	for i := range want {
		assert.True(t, got[i].equal(want[i]), "step %d: got %+v want %+v", i, got[i], want[i])
	}
}

func TestChatPlaceholdersAndChaining(t *testing.T) {
	enter, ctrlA := key("Enter"), key("KeyA", platform.Ctrl)
	known := chatContext{last: "Buyer", area: "Felled Hideout"}
	assert.Equal(t, []string{"@last ty", "/kick @last"}, chatLines(" @last ty&& /kick @last &&"))
	last, area := chatPlaceholders("/hideout @last")
	assert.True(t, last)
	assert.False(t, area)

	// Known whisper: a leading @last whispers, elsewhere it is the bare name. All but the last
	// line are sent even without send.
	steps, err := chatSteps("@last ty @last&&/kick @last", false, known)
	assertSteps(t, steps, err, enter, ctrlA, pasteStep("@Buyer ty Buyer"), enter, enter, ctrlA, pasteStep("/kick Buyer"))
	steps, err = chatSteps("Now in @area", true, known)
	assertSteps(t, steps, err, enter, ctrlA, pasteStep("Now in Felled Hideout"), enter)

	// No log: Ctrl+Enter makes the game type `@Name `.
	none := chatContext{}
	steps, err = chatSteps("@last ty", true, none)
	assertSteps(t, steps, err, key("Enter", platform.Ctrl), key("End"), pasteStep("ty"), enter)
	steps, err = chatSteps("/invite @last", true, none)
	assertSteps(t, steps, err, key("Enter", platform.Ctrl), key("Home"), key("Delete"), pasteStep("/invite "), enter)
	_, err = chatSteps("@last hi @last", true, none)
	require.ErrorIs(t, err, errChatNoWhisper)
	_, err = chatSteps("in @area", true, none)
	require.ErrorIs(t, err, errChatNoArea)
}

func TestChatStashAndWheelSteps(t *testing.T) {
	enter, ctrlA := key("Enter"), key("KeyA", platform.Ctrl)
	none := chatContext{}
	steps, err := chatSteps("/hideout", true, none)
	assertSteps(t, steps, err, enter, ctrlA, pasteStep("/hideout"), enter)
	steps, err = chatSteps("hi", false, none)
	assertSteps(t, steps, err, enter, ctrlA, pasteStep("hi"))
	steps, err = chatSteps(" && ", true, none)
	assertSteps(t, steps, err)
	assertSteps(t, stashSteps(`"Rarity: Rare"`), nil,
		key("KeyF", platform.Ctrl), key("KeyA", platform.Ctrl), pasteStep(`"Rarity: Rare"`), enter)

	for delta, want := range map[int32]string{1: "ArrowLeft", -3: "ArrowRight", 0: ""} {
		k, ok := wheelTabKey(delta)
		assert.Equal(t, want, k, delta)
		assert.Equal(t, want != "", ok, delta)
	}
	assert.True(t, isCtrlKey("ControlLeft"))
	assert.True(t, isCtrlKey("ControlRight"))
	assert.False(t, isCtrlKey("ShiftLeft"))
	assert.True(t, isCloseKey("Escape"))
	assert.False(t, isCloseKey("KeyD"))
}
