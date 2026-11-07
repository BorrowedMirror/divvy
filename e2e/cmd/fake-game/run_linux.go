//go:build linux

package main

import (
	"errors"
	"os"
)

// appName is the X11 WM_CLASS and the Wayland app id (winit used the program name).
const appName = "fake-game"

// run opens the window on Wayland when WAYLAND_DISPLAY is set (like winit), else on X11.
func run(g *game, w, h uint32) error {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return runWayland(g, w, h)
	}
	if os.Getenv("DISPLAY") == "" {
		return errors.New("no display: neither WAYLAND_DISPLAY nor DISPLAY is set")
	}
	return runX11(g, w, h)
}
