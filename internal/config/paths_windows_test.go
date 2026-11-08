package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveWindowsLayout(t *testing.T) {
	t.Setenv(ConfigDirEnv, "")
	t.Setenv("APPDATA", `C:\Users\Exile\AppData\Roaming`)
	t.Setenv("LOCALAPPDATA", `C:\Users\Exile\AppData\Local`)
	got, err := Resolve()
	require.NoError(t, err)
	assert.Equal(t, Paths{
		ConfigDir:    `C:\Users\Exile\AppData\Roaming\rkl\config`,
		CacheDir:     `C:\Users\Exile\AppData\Local\rkl\cache`,
		LogDir:       `C:\Users\Exile\AppData\Local\rkl\data\logs`,
		SettingsFile: `C:\Users\Exile\AppData\Roaming\rkl\config\settings.toml`,
	}, got)

	legacy, ok := LegacySettingsFile()
	require.True(t, ok)
	assert.Equal(t, `C:\Users\Exile\AppData\Roaming\nerv\kalandra\config\settings.toml`, legacy)
}

func TestResolveWithoutAppDataIsNoConfigDir(t *testing.T) {
	t.Setenv(ConfigDirEnv, "")
	t.Setenv("APPDATA", `C:\x`)
	t.Setenv("LOCALAPPDATA", "")
	_, err := Resolve()
	require.ErrorIs(t, err, ErrNoConfigDir)
}
