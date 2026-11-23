package config

import (
	"os"
	"path/filepath"
)

// The app's directory under ~/Library/Application Support and ~/Library/Caches (the
// bundle identifier), now and before the rename.
const (
	appDirName    = "app.divvy"
	legacyDirName = "au.nerv.kalandra"
)

func dirsFor(name string) (userDirs, error) {
	support, err := os.UserConfigDir() // ~/Library/Application Support
	if err != nil {
		return userDirs{}, err
	}
	caches, err := os.UserCacheDir() // ~/Library/Caches
	if err != nil {
		return userDirs{}, err
	}
	config := filepath.Join(support, name)
	return userDirs{
		config: config,
		cache:  filepath.Join(caches, name),
		logs:   filepath.Join(config, "logs"),
	}, nil
}
