//go:build !windows && !darwin

package config

import (
	"os"
	"path/filepath"
)

// The app's directory name under each XDG base directory, now and before the rename.
const (
	appDirName    = "rkl"
	legacyDirName = "kalandra"
)

func dirsFor(name string) (userDirs, error) {
	config, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return userDirs{}, err
	}
	cache, err := xdgDir("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return userDirs{}, err
	}
	state, err := xdgDir("XDG_STATE_HOME", ".local/state")
	if err != nil {
		return userDirs{}, err
	}
	return userDirs{
		config: filepath.Join(config, name),
		cache:  filepath.Join(cache, name),
		logs:   filepath.Join(state, name, "logs"),
	}, nil
}

// xdgDir is the XDG base directory in the environment variable env, or home/fallback when
// the variable is unset or relative (the XDG spec says to ignore relative paths, where
// [os.UserConfigDir] returns an error, and it has no state directory).
func xdgDir(env, fallback string) (string, error) {
	if dir := os.Getenv(env); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}
