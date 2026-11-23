package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveHonoursEnvOverride(t *testing.T) {
	root := filepath.FromSlash("/portable/divvy")
	t.Setenv(ConfigDirEnv, root)
	paths, err := Resolve()
	require.NoError(t, err)
	assert.Equal(t, Under(root), paths)
	assert.Equal(t, filepath.FromSlash("/portable/divvy/config/settings.toml"), paths.SettingsFile)
}

func TestResolveIgnoresBlankOverride(t *testing.T) {
	t.Setenv(ConfigDirEnv, "  ")
	blank, err := Resolve()
	require.NoError(t, err)
	t.Setenv(ConfigDirEnv, "")
	unset, err := Resolve()
	require.NoError(t, err)
	assert.Equal(t, unset, blank)
	assert.NotEqual(t, Under("  "), blank)
}
