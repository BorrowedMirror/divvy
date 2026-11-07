//go:build !linux && !windows && !darwin

package main

import (
	"errors"
	"runtime"
)

func run(*game, uint32, uint32) error {
	return errors.New("fake-game does not support " + runtime.GOOS)
}
