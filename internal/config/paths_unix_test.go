//go:build !windows && !darwin

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveXDGLayouts(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Paths
	}{
		{
			name: "defaults",
			env:  map[string]string{"HOME": "/home/exile"},
			want: Paths{
				ConfigDir:    "/home/exile/.config/divvy",
				CacheDir:     "/home/exile/.cache/divvy",
				LogDir:       "/home/exile/.local/state/divvy/logs",
				SettingsFile: "/home/exile/.config/divvy/settings.toml",
			},
		},
		{
			name: "xdg overrides",
			env: map[string]string{
				"HOME": "/home/exile", "XDG_CONFIG_HOME": "/cfg", "XDG_CACHE_HOME": "/var/cache/",
				"XDG_STATE_HOME": "/st",
			},
			want: Paths{
				ConfigDir:    "/cfg/divvy",
				CacheDir:     "/var/cache/divvy",
				LogDir:       "/st/divvy/logs",
				SettingsFile: "/cfg/divvy/settings.toml",
			},
		},
		{
			name: "relative xdg ignored",
			env:  map[string]string{"HOME": "/home/exile", "XDG_CONFIG_HOME": "cfg"},
			want: Paths{
				ConfigDir:    "/home/exile/.config/divvy",
				CacheDir:     "/home/exile/.cache/divvy",
				LogDir:       "/home/exile/.local/state/divvy/logs",
				SettingsFile: "/home/exile/.config/divvy/settings.toml",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setUserEnv(t, c.env)
			got, err := Resolve()
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
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
