package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths are the resolved per-OS locations.
type Paths struct {
	ConfigDir    string
	CacheDir     string
	LogDir       string
	SettingsFile string
}

// ConfigDirEnv relocates every path under one root (portable mode, tests).
const ConfigDirEnv = "RKL_CONFIG_DIR"

// userDirs are the per-user directories of one app.
type userDirs struct {
	config, cache, logs string
}

// Resolve resolves the paths from the process environment: $RKL_CONFIG_DIR (when not
// blank) roots everything there ([Under]); otherwise they are the per-user directories for
// this OS:
//
//   - Linux: $XDG_CONFIG_HOME/rkl, $XDG_CACHE_HOME/rkl and $XDG_STATE_HOME/rkl/logs
//     (~/.config, ~/.cache and ~/.local/state when unset or relative);
//   - Windows: %APPDATA%\rkl\config, %LOCALAPPDATA%\rkl\cache and
//     %LOCALAPPDATA%\rkl\data\logs;
//   - macOS: ~/Library/Application Support/app.rkl (logs in its `logs`) and
//     ~/Library/Caches/app.rkl, after the bundle identifier `app.rkl`.
func Resolve() (Paths, error) {
	if root := os.Getenv(ConfigDirEnv); strings.TrimSpace(root) != "" {
		return Under(root), nil
	}
	d, err := dirsFor(appDirName)
	if err != nil {
		return Paths{}, fmt.Errorf("%w: %w", ErrNoConfigDir, err)
	}
	return Paths{
		ConfigDir:    d.config,
		CacheDir:     d.cache,
		LogDir:       d.logs,
		SettingsFile: filepath.Join(d.config, "settings.toml"),
	}, nil
}

// Under roots all paths under root (tests, portable mode).
func Under(root string) Paths {
	return Paths{
		ConfigDir:    filepath.Join(root, "config"),
		CacheDir:     filepath.Join(root, "cache"),
		LogDir:       filepath.Join(root, "logs"),
		SettingsFile: filepath.Join(root, "config", "settings.toml"),
	}
}

// LegacySettingsFile is where settings lived before the project was renamed from kalandra
// (bundle identifier `au.nerv.kalandra`); false when no home directory is known.
func LegacySettingsFile() (string, bool) {
	d, err := dirsFor(legacyDirName)
	if err != nil {
		return "", false
	}
	return filepath.Join(d.config, "settings.toml"), true
}
