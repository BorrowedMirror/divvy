package autostart

import (
	"fmt"
	"os"
)

// set writes or removes `$XDG_CONFIG_HOME/autostart/divvy.desktop` (`~/.config` by default).
func set(enabled bool, path string) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("no config directory for the autostart entry: %w", err)
	}
	entry := desktopEntryPath(dir)
	if enabled {
		return writeFile(entry, desktopEntry(path))
	}
	return removeFile(entry)
}
