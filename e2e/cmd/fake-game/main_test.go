package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noEnv(string) string { return "" }

func mustArgs(t *testing.T, args ...string) options {
	t.Helper()
	o, err := parseArgs(args, noEnv)
	require.NoError(t, err)
	return o
}

func TestChords(t *testing.T) {
	it := items{normal: "N", advanced: "A"}
	ctrl, super, alt := mods{ctrl: true}, mods{super: true}, mods{alt: true}
	cases := []struct {
		name string
		ev   keyEvent
		want string
		ok   bool
	}{
		{"ctrl+c", keyEvent{isC: true, mods: ctrl}, "N", true},
		{"cmd+c", keyEvent{isC: true, mods: super}, "N", true},
		{"ctrl+alt+c", keyEvent{isC: true, mods: mods{ctrl: true, alt: true}}, "A", true},
		{"c", keyEvent{isC: true}, "", false},
		{"alt+c", keyEvent{isC: true, mods: alt}, "", false},
		{"ctrl+d", keyEvent{mods: ctrl}, "", false},
	}
	for _, c := range cases {
		got, ok := it.forKey(c.ev)
		assert.Equal(t, c.ok, ok, c.name)
		assert.Equal(t, c.want, got, c.name)
	}
}

type fakeClipboard struct {
	set   []string
	paste string
	fail  bool
}

func (f *fakeClipboard) setText(s string) error {
	if f.fail {
		return errors.New("boom")
	}
	f.set = append(f.set, s)
	return nil
}

func (f *fakeClipboard) text() (string, error) { return f.paste, nil }

// captureStdout runs fn and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	require.NoError(t, w.Close())
	b := make([]byte, 1<<16)
	n, _ := r.Read(b)
	return string(b[:n])
}

func TestCopyOnPressOrOrphanRelease(t *testing.T) {
	g := &game{items: items{normal: "N", advanced: "A"}}
	cb := &fakeClipboard{}
	ctrlC := keyEvent{isC: true, code: "KeyC", mods: mods{ctrl: true}}
	out := captureStdout(t, func() {
		press, release := ctrlC, ctrlC
		press.pressed = true
		assert.True(t, g.key(press, cb))
		assert.False(t, g.key(release, cb), "release after a seen press")
		// The press got lost (keymap swap): copy on the orphan release.
		assert.True(t, g.key(release, cb))
		g.drawn()
		g.drawn()
	})
	assert.Equal(t, []string{"N", "N"}, cb.set)
	assert.Equal(t, "COPIED 1\nCOPIED 2\nREADY\n", out)
	assert.Equal(t, uint32(0x221810), g.color())

	cb.fail = true
	out = captureStdout(t, func() { assert.False(t, g.key(keyEvent{pressed: true, isC: true, mods: mods{ctrl: true}}, cb)) })
	assert.Empty(t, out, "a failed copy is not reported")
}

func TestLogKeys(t *testing.T) {
	g := &game{items: items{normal: "N", advanced: "N"}, logKeys: true}
	cb := &fakeClipboard{paste: "pasted"}
	out := captureStdout(t, func() {
		g.key(keyEvent{pressed: true, code: "KeyA", mods: mods{shift: true}}, cb)
		g.key(keyEvent{pressed: false, code: "KeyA"}, cb)
		g.key(keyEvent{pressed: true, isV: true, code: "KeyV", mods: mods{ctrl: true}}, cb)
		g.key(keyEvent{pressed: true, isC: true, code: "KeyC", mods: mods{ctrl: true, alt: true}}, cb)
	})
	assert.Equal(t, "KEY shift+KeyA\nKEY ctrl+KeyV\nPASTE pasted\nKEY ctrl+alt+KeyC\nCOPIED 1\n", out)
}

func TestCorpusIDResolvesAndAdvancedDefaultsToNormal(t *testing.T) {
	it, err := loadItems(mustArgs(t, "--corpus-id", "currency/divine-orb"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(it.normal, "Item Class:"), it.normal)
	assert.Equal(t, it.normal, it.advanced)
}

func TestFilesAndErrors(t *testing.T) {
	dir := t.TempDir()
	n, a := filepath.Join(dir, "n.txt"), filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(n, []byte("normal"), 0o600))
	require.NoError(t, os.WriteFile(a, []byte("adv"), 0o600))

	it, err := loadItems(mustArgs(t, "--item", n, "--advanced-item", a))
	require.NoError(t, err)
	assert.Equal(t, items{normal: "normal", advanced: "adv"}, it)

	it, err = loadItems(mustArgs(t, "--item", n, "--advanced-corpus-id", "a", "--corpus-dir", dir))
	require.NoError(t, err)
	assert.Equal(t, "adv", it.advanced)

	// DIVVY_CORPUS_DIR, unless --corpus-dir is given.
	o, err := parseArgs([]string{"--corpus-id", "a"}, func(k string) string {
		if k == "DIVVY_CORPUS_DIR" {
			return dir
		}
		return ""
	})
	require.NoError(t, err)
	it, err = loadItems(o)
	require.NoError(t, err)
	assert.Equal(t, "adv", it.normal)

	_, err = loadItems(mustArgs(t))
	require.Error(t, err)
	_, err = loadItems(mustArgs(t, "--item", "/nonexistent/x.txt"))
	require.Error(t, err)
	_, err = parseArgs([]string{"--item", "x", "--corpus-id", "y"}, noEnv)
	require.Error(t, err)
	_, err = parseArgs([]string{"--advanced-item", "x", "--advanced-corpus-id", "y"}, noEnv)
	require.Error(t, err)
	_, err = parseArgs([]string{"stray"}, noEnv)
	require.Error(t, err)
}

func TestSizes(t *testing.T) {
	w, h, err := parseSize("640x480")
	require.NoError(t, err)
	assert.Equal(t, [2]uint32{640, 480}, [2]uint32{w, h})
	for _, bad := range []string{"640", "0x5", "ax5", "5xb"} {
		_, _, err := parseSize(bad)
		require.Error(t, err, bad)
	}
	o := mustArgs(t, "--exit-after", "1.5")
	assert.True(t, o.hasExitAfter)
	assert.InDelta(t, 1.5, o.exitAfter, 0)
	assert.Equal(t, [2]uint32{1280, 720}, [2]uint32{o.width, o.height})
	o = mustArgs(t, "--size", "640x480")
	assert.Equal(t, [2]uint32{640, 480}, [2]uint32{o.width, o.height})
	_, err = parseArgs([]string{"--size", "0x1"}, noEnv)
	require.Error(t, err)
}

func TestCharCode(t *testing.T) {
	assert.Equal(t, "KeyC", charCode('c'))
	assert.Equal(t, "KeyV", charCode('V'))
	assert.Equal(t, "Digit7", charCode('7'))
	assert.Empty(t, charCode('-'))
}
