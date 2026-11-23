package autostart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetTogglesTheXDGEntry(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("APPIMAGE", "/home/u/Apps/divvy.AppImage")
	entry := filepath.Join(cfg, "autostart", "divvy.desktop")
	require.NoError(t, Set(true))
	b, err := os.ReadFile(entry)
	require.NoError(t, err)
	assert.Equal(t, desktopEntry("/home/u/Apps/divvy.AppImage"), string(b))
	for range 2 {
		require.NoError(t, Set(false))
	}
	_, err = os.Stat(entry)
	require.ErrorIs(t, err, os.ErrNotExist, "entry removed")

	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	require.NoError(t, Set(true))
	assert.FileExists(t, filepath.Join(home, ".config", "autostart", "divvy.desktop"), "~/.config without XDG_CONFIG_HOME")

	t.Setenv("HOME", "")
	assert.Error(t, Set(true), "no config directory")
}
