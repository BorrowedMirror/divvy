package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveMacOSLayout(t *testing.T) {
	t.Setenv(ConfigDirEnv, "")
	t.Setenv("HOME", "/Users/exile")
	t.Setenv("XDG_CONFIG_HOME", "/ignored")
	got, err := Resolve()
	require.NoError(t, err)
	assert.Equal(t, Paths{
		ConfigDir:    "/Users/exile/Library/Application Support/app.rkl",
		CacheDir:     "/Users/exile/Library/Caches/app.rkl",
		LogDir:       "/Users/exile/Library/Application Support/app.rkl/logs",
		SettingsFile: "/Users/exile/Library/Application Support/app.rkl/settings.toml",
	}, got)

	legacy, ok := LegacySettingsFile()
	require.True(t, ok)
	assert.Equal(t, "/Users/exile/Library/Application Support/au.nerv.kalandra/settings.toml", legacy)
}

func TestResolveWithoutHomeIsNoConfigDir(t *testing.T) {
	t.Setenv(ConfigDirEnv, "")
	t.Setenv("HOME", "")
	_, err := Resolve()
	require.ErrorIs(t, err, ErrNoConfigDir)
}
