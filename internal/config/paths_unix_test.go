//go:build !windows && !darwin

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveXDGLayouts(t *testing.T) {
	testCases := []struct {
		name     string
		env      map[string]string
		expected Paths
	}{
		{
			"UsesDefaults",
			map[string]string{"HOME": "/home/exile"},
			Paths{
				ConfigDir:    "/home/exile/.config/divvy",
				CacheDir:     "/home/exile/.cache/divvy",
				LogDir:       "/home/exile/.local/state/divvy/logs",
				SettingsFile: "/home/exile/.config/divvy/settings.toml",
			},
		},
		{
			"HonoursXDGOverrides",
			map[string]string{
				"HOME": "/home/exile", "XDG_CONFIG_HOME": "/cfg", "XDG_CACHE_HOME": "/var/cache/",
				"XDG_STATE_HOME": "/st",
			},
			Paths{
				ConfigDir:    "/cfg/divvy",
				CacheDir:     "/var/cache/divvy",
				LogDir:       "/st/divvy/logs",
				SettingsFile: "/cfg/divvy/settings.toml",
			},
		},
		{
			"IgnoresRelativeXDG",
			map[string]string{"HOME": "/home/exile", "XDG_CONFIG_HOME": "cfg"},
			Paths{
				ConfigDir:    "/home/exile/.config/divvy",
				CacheDir:     "/home/exile/.cache/divvy",
				LogDir:       "/home/exile/.local/state/divvy/logs",
				SettingsFile: "/home/exile/.config/divvy/settings.toml",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setUserEnv(t, tc.env)
			got, err := Resolve()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestResolveWithoutHomeIsNoConfigDir(t *testing.T) {
	setUserEnv(t, map[string]string{"HOME": ""})
	_, err := Resolve()
	require.ErrorIs(t, err, ErrNoConfigDir)
	_, ok := LegacySettingsFile()
	assert.False(t, ok)
}

func TestLegacySettingsFileXDG(t *testing.T) {
	setUserEnv(t, map[string]string{"HOME": "/home/exile"})
	got, ok := LegacySettingsFile()
	require.True(t, ok)
	assert.Equal(t, "/home/exile/.config/kalandra/settings.toml", got)

	setUserEnv(t, map[string]string{"HOME": "/home/exile", "XDG_CONFIG_HOME": "/cfg"})
	got, ok = LegacySettingsFile()
	require.True(t, ok)
	assert.Equal(t, "/cfg/kalandra/settings.toml", got)
}

// setUserEnv sets the variables the paths depend on to env, clearing the others.
func setUserEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{ConfigDirEnv, "HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(k, env[k])
	}
}
