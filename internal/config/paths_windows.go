package config

import (
	"os"
	"path/filepath"
)

// The app's directory under %APPDATA% and %LOCALAPPDATA%, now and before the rename.
const (
	appDirName    = "divvy"
	legacyDirName = `nerv\kalandra`
)

func dirsFor(name string) (userDirs, error) {
	roaming, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		return userDirs{}, err
	}
	local, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		return userDirs{}, err
	}
	return userDirs{
		config: filepath.Join(roaming, name, "config"),
		cache:  filepath.Join(local, name, "cache"),
		logs:   filepath.Join(local, name, "data", "logs"),
	}, nil
}
