package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorKindsAndMessages(t *testing.T) {
	err := Timeout(2 * time.Second)
	require.ErrorIs(t, err, ErrTimeout)
	require.NotErrorIs(t, err, ErrBackend)
	assert.Equal(t, "timed out after 2s", err.Error())
	assert.Equal(t, "invalid key chord `a+b`", InvalidChord("a+b").Error())
	assert.Equal(t, "capability `global_hotkeys` is not supported on this backend", Unsupported("global_hotkeys").Error())
	wrapped := errors.Join(errors.New("context"), HotkeyConflict("Ctrl+D"))
	require.ErrorIs(t, wrapped, ErrHotkeyConflict)
}

func TestBackendErrorsWrapTheirCause(t *testing.T) {
	cause := errors.New("connection refused")
	err := Backend("x11: %w", cause)
	require.ErrorIs(t, err, ErrBackend)
	require.ErrorIs(t, err, cause)
	require.NotErrorIs(t, err, ErrTimeout)
	assert.Equal(t, "backend error: x11: connection refused", err.Error())
	assert.Equal(t, "backend error: plain", Backend("plain").Error())
}

func TestUnavailablePartsFailAsUnsupported(t *testing.T) {
	u := Unavailable("passive_hook")
	_, err := u.Start(t.Context())
	require.ErrorIs(t, err, ErrUnsupported)
	assert.Equal(t, "capability `passive_hook` is not supported on this backend", err.Error())
	_, err = u.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorIs(t, u.SendChord(NewChord("KeyC")), ErrUnsupported)
	_, found := u.GameWindow()
	assert.False(t, found)
	assert.True(t, u.IsGameFocused())
}
