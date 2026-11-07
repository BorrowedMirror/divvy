package app

import (
	"errors"
	"os"
)

func setAutostart(enabled bool, path string) error {
	entry, ok := LinuxAutostartPath(os.Getenv("XDG_CONFIG_HOME"), os.Getenv("HOME"))
	if !ok {
		return errors.New("no config directory for the autostart entry")
	}
	if enabled {
		return writeAutostartFile(entry, DesktopEntry(path))
	}
	return removeAutostartFile(entry)
}
