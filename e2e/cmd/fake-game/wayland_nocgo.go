//go:build linux && !cgo

package main

import "errors"

func runWayland(*game, uint32, uint32) error {
	return errors.New("native Wayland needs a cgo build (libwayland-client, xkbcommon); unset WAYLAND_DISPLAY to use X11")
}
