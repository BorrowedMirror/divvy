package platform

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLettersDigitsAndNamedKeys(t *testing.T) {
	cases := []struct {
		in   string
		want Chord
	}{
		{"Ctrl+Alt+D", NewChord("KeyD", Ctrl, Alt)},
		{"control + 1", NewChord("Digit1", Ctrl)},
		{"shift+F5", NewChord("F5", Shift)},
		{"cmd+option+KeyX", NewChord("KeyX", Meta, Alt)},
	}
	for _, c := range cases {
		got, err := ParseChord(c.in)
		require.NoError(t, err, c.in)
		assert.True(t, got.Equal(c.want), "%s: got %v", c.in, got)
	}
	got, err := ParseChord("super+win+d")
	require.NoError(t, err)
	assert.Equal(t, []KeyModifier{Meta, Meta}, got.Modifiers)
}

func TestParseRejectsBadChords(t *testing.T) {
	for _, s := range []string{"", "ctrl", "ctrl+alt", "a+b", "+++"} {
		_, err := ParseChord(s)
		require.ErrorIs(t, err, ErrInvalidChord, s)
	}
}

func TestDisplayRoundtrips(t *testing.T) {
	for _, s := range []string{"Ctrl+Alt+D", "Shift+F5", "Meta+1", "Ctrl+Shift+Alt+Escape"} {
		c, err := ParseChord(s)
		require.NoError(t, err)
		assert.Equal(t, s, c.String())
		again, err := ParseChord(c.String())
		require.NoError(t, err)
		assert.True(t, again.Equal(c))
	}
}

func TestUnboundChord(t *testing.T) {
	assert.True(t, UnboundChord().IsUnbound())
	assert.True(t, NewChord("  ").IsUnbound())
	assert.False(t, NewChord("KeyD", Ctrl).IsUnbound())
	b, err := json.Marshal(UnboundChord())
	require.NoError(t, err)
	assert.JSONEq(t, `{"modifiers":[],"key":""}`, string(b))
}

func TestPlanKeepsHeldChordModifiers(t *testing.T) {
	// Hotkey Ctrl+D held, sending Ctrl+C: nothing to press or release.
	assert.Equal(t, ChordPlan{}, PlanChord([]KeyModifier{Ctrl}, []KeyModifier{Ctrl}))
	// Sending Ctrl+Alt+C while Ctrl held: press Alt only.
	assert.Equal(t, ChordPlan{Press: []KeyModifier{Alt}}, PlanChord([]KeyModifier{Ctrl, Alt}, []KeyModifier{Ctrl}))
	// Hotkey Ctrl+Shift+D: Shift must go up first.
	assert.Equal(t, ChordPlan{ReleaseFirst: []KeyModifier{Shift}}, PlanChord([]KeyModifier{Ctrl}, []KeyModifier{Ctrl, Shift}))
	// Nothing held (or unknown, e.g. Wayland).
	assert.Equal(t, ChordPlan{Press: []KeyModifier{Ctrl, Alt}}, PlanChord([]KeyModifier{Ctrl, Alt}, nil))
	// Duplicates collapse.
	assert.Equal(t,
		ChordPlan{ReleaseFirst: []KeyModifier{Shift}, Press: []KeyModifier{Ctrl}},
		PlanChord([]KeyModifier{Ctrl, Ctrl}, []KeyModifier{Shift, Shift}))
}

var modName = map[KeyModifier]string{Ctrl: "Ctrl", Alt: "Alt", Shift: "Shift", Meta: "Meta"}

// keyLog records every key operation and fails the one named failOn.
type keyLog struct {
	ops    []string
	failOn string
}

func (l *keyLog) op(s string) error {
	l.ops = append(l.ops, s)
	if l.failOn != "" && l.failOn == s {
		return errors.New(s)
	}
	return nil
}

func (l *keyLog) ReleaseHeld(m KeyModifier) error { return l.op("up-held " + modName[m]) }
func (l *keyLog) Press(m KeyModifier) error       { return l.op("down " + modName[m]) }
func (l *keyLog) Release(m KeyModifier) error     { return l.op("up " + modName[m]) }
func (l *keyLog) Click() error                    { return l.op("click") }

func TestRunReleasesExtrasThenPressesMissingInOrder(t *testing.T) {
	log := &keyLog{}
	require.NoError(t, RunChord([]KeyModifier{Ctrl, Alt}, []KeyModifier{Ctrl, Shift}, log))
	assert.Equal(t, []string{"up-held Shift", "down Alt", "click", "up Alt"}, log.ops)

	log = &keyLog{}
	require.NoError(t, RunChord([]KeyModifier{Ctrl, Alt}, nil, log))
	assert.Equal(t, []string{"down Ctrl", "down Alt", "click", "up Alt", "up Ctrl"}, log.ops)
}

func TestRunAlwaysReleasesWhatItPressed(t *testing.T) {
	log := &keyLog{failOn: "down Alt"}
	require.EqualError(t, RunChord([]KeyModifier{Ctrl, Alt}, nil, log), "down Alt")
	assert.Equal(t, []string{"down Ctrl", "down Alt", "up Ctrl"}, log.ops)

	log = &keyLog{failOn: "click"}
	require.EqualError(t, RunChord([]KeyModifier{Ctrl}, nil, log), "click")
	assert.Equal(t, []string{"down Ctrl", "click", "up Ctrl"}, log.ops)

	log = &keyLog{failOn: "up Ctrl"}
	require.EqualError(t, RunChord([]KeyModifier{Ctrl}, nil, log), "up Ctrl")

	log = &keyLog{failOn: "up-held Shift"}
	require.Error(t, RunChord([]KeyModifier{Ctrl}, []KeyModifier{Shift}, log))
	assert.Equal(t, []string{"up-held Shift"}, log.ops, "nothing pressed after a failed release")
}
