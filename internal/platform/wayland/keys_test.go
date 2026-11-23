package wayland

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func mustChord(t *testing.T, s string) platform.Chord {
	t.Helper()
	c, err := platform.ParseChord(s)
	require.NoError(t, err)
	return c
}

func TestBuildsKeySequence(t *testing.T) {
	args, err := keyArgs(mustChord(t, "Ctrl+Alt+C"))
	require.NoError(t, err)
	assert.Equal(t, []string{"key", "29:1", "56:1", "46:1", "46:0", "56:0", "29:0"}, args)
	_, err = keyArgs(mustChord(t, "ctrl+Pause"))
	require.ErrorIs(t, err, platform.ErrInvalidChord)
	// No held modifiers on Wayland: a bare key is just down/up; duplicates collapse (chordplan).
	args, err = keyArgs(platform.NewChord("Enter"))
	require.NoError(t, err)
	assert.Equal(t, []string{"key", "28:1", "28:0"}, args)
	args, err = keyArgs(platform.NewChord("KeyV", platform.Ctrl, platform.Ctrl))
	require.NoError(t, err)
	assert.Equal(t, []string{"key", "29:1", "47:1", "47:0", "29:0"}, args)
}

func TestEvdevCodes(t *testing.T) {
	code := func(k string) any {
		if c, ok := evdevCode(k); ok {
			return c
		}
		return nil
	}
	assert.Equal(t, uint16(30), code("KeyA"))
	assert.Equal(t, uint16(44), code("KeyZ"))
	assert.Equal(t, uint16(11), code("Digit0"))
	assert.Equal(t, uint16(2), code("Digit1"))
	assert.Equal(t, uint16(59), code("F1"))
	assert.Equal(t, uint16(68), code("F10"))
	assert.Equal(t, uint16(88), code("F12"))
	assert.Nil(t, code("F13"))
	assert.Equal(t, uint16(57), code("Space"))
	assert.Equal(t, []any{uint16(105), uint16(106)}, []any{code("ArrowLeft"), code("ArrowRight")})
	assert.Equal(t, []any{uint16(102), uint16(107), uint16(111)}, []any{code("Home"), code("End"), code("Delete")})
	assert.Equal(t, []any{uint16(104), uint16(109)}, []any{code("PageUp"), code("PageDown")})
	assert.Nil(t, code("Keya"))
	assert.Nil(t, code("Key1"))
	assert.Nil(t, code("Digit10"))
	assert.Equal(t, uint16(42), modifierCode(platform.Shift))
	assert.Equal(t, uint16(125), modifierCode(platform.Meta))
}

func TestRunsAFakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs /bin/true and /bin/false")
	}
	chord := mustChord(t, "ctrl+c")
	require.NoError(t, (&ydotool{binary: "/bin/true"}).SendChord(chord))
	err := (&ydotool{binary: "/bin/false"}).SendChord(chord)
	require.ErrorIs(t, err, platform.ErrBackend)
	err = (&ydotool{binary: filepath.Join(t.TempDir(), "nope")}).SendChord(chord)
	require.ErrorIs(t, err, platform.ErrBackend)
	err = (&ydotool{binary: "/bin/true"}).SendChord(platform.NewChord("NoSuchKey"))
	require.ErrorIs(t, err, platform.ErrInvalidChord)

	// The fake records its arguments: the exact `ydotool key` command line.
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	fake := filepath.Join(dir, "ydotool")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\necho \"$@\" > "+out+"\n"), 0o755))
	require.NoError(t, (&ydotool{binary: fake}).SendChord(mustChord(t, "Ctrl+Alt+C")))
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "key 29:1 56:1 46:1 46:0 56:0 29:0\n", string(got))

	t.Setenv("PATH", "/nonexistent"+string(filepath.ListSeparator)+dir)
	yd := detectYdotool()
	require.NotNil(t, yd)
	assert.Equal(t, fake, yd.binary)
	// Not executable: skipped.
	require.NoError(t, os.Chmod(fake, 0o644))
	assert.Nil(t, detectYdotool())
}
