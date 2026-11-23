package x11

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// noDisplay is a display name no X server can have.
const noDisplay = "divvy-disabled-display"

type countingSim struct{ calls int }

func (c *countingSim) SendChord(platform.Chord) error { c.calls++; return nil }

func TestLazyConnectionRetriesAndUsesFallback(t *testing.T) {
	sim := newSimulator(noDisplay)
	chord := platform.NewChord("KeyC", platform.Ctrl)
	require.Error(t, sim.ensureReady(), "cannot connect")
	require.ErrorIs(t, sim.SendChord(chord), platform.ErrBackend, "retried, still failing")
	assert.False(t, sim.ready())
	require.ErrorIs(t, sim.SendChord(platform.NewChord("NoSuchKey")), platform.ErrInvalidChord)

	fallback := &countingSim{}
	sim = newSimulator(noDisplay)
	sim.fallback = fallback
	for range 2 {
		require.NoError(t, sim.SendChord(chord))
	}
	assert.Equal(t, 2, fallback.calls)
}

func TestNewFailsWithoutXServer(t *testing.T) {
	_, err := New(Options{Display: noDisplay})
	require.Error(t, err)
}
