//go:build !linux && !darwin && !windows

package autostart

import "errors"

func set(bool, string) error {
	return errors.New("start at login is not supported on this platform")
}
