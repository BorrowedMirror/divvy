package app

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// autostartRunKey is `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.
const autostartRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func setAutostart(enabled bool, path string) error {
	if enabled {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		// Quoted, as `reg add ... /d "\"path\""` wrote it in the Rust build.
		return k.SetStringValue(AutostartName, `"`+path+`"`)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	// Deleting a missing value is "already off".
	if err := k.DeleteValue(AutostartName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
