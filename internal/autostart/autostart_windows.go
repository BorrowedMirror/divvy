package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// runKey is `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// set writes or deletes the `rkl` value of the Run key, holding the quoted program path.
func set(enabled bool, path string) error {
	if enabled {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue(name, `"`+path+`"`)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	// Deleting a missing value is "already off".
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
