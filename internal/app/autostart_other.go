//go:build !linux && !darwin && !windows

package app

import "errors"

func setAutostart(bool, string) error {
	return errors.New("start at login is not supported on this platform")
}
