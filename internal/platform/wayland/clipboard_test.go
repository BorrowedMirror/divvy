//go:build linux

package wayland

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type call struct {
	name  string
	stdin string
	args  []string
}

func fakeClipboard(reply func(c call) ([]byte, error)) (*wlClipboard, *[]call) {
	var calls []call
	return &wlClipboard{paste: "wl-paste", copy: "wl-copy", run: func(name string, stdin []byte, args ...string) ([]byte, error) {
		c := call{name: name, stdin: string(stdin), args: args}
		calls = append(calls, c)
		return reply(c)
	}}, &calls
}

func TestWLClipboardReadsText(t *testing.T) {
	c, calls := fakeClipboard(func(call) ([]byte, error) { return []byte("Item Class: Rings\n"), nil })
	text, ok, err := c.Text()
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "Item Class: Rings\n", text)
	assert.Equal(t, []string{"--no-newline", "--type", "text"}, (*calls)[0].args)
}

func TestWLClipboardEmptyIsNotAnError(t *testing.T) {
	c, _ := fakeClipboard(func(call) ([]byte, error) { return nil, errors.New("Nothing is copied") })
	_, ok, err := c.Text()
	require.NoError(t, err)
	assert.False(t, ok)

	c, _ = fakeClipboard(func(call) ([]byte, error) { return nil, errors.New("compositor does not support data-control") })
	_, _, err = c.Text()
	assert.Error(t, err)
}

func TestWLClipboardWritesAndClears(t *testing.T) {
	c, calls := fakeClipboard(func(call) ([]byte, error) { return nil, nil })
	require.NoError(t, c.SetText("@player hi"))
	require.NoError(t, c.Clear())
	assert.Equal(t, call{name: "wl-copy", stdin: "@player hi", args: []string{"--type", "text/plain;charset=utf-8"}}, (*calls)[0])
	assert.Equal(t, []string{"--clear"}, (*calls)[1].args)
}

func TestWLClipboardDetectsNonText(t *testing.T) {
	for types, want := range map[string]bool{
		"image/png\n":                         true,
		"image/png\ntext/plain;charset=utf-8": false,
		"UTF8_STRING\nTEXT\n":                 false,
		"":                                    false,
	} {
		c, _ := fakeClipboard(func(call) ([]byte, error) { return []byte(types), nil })
		assert.Equal(t, want, c.HasNonText(), types)
	}
}
