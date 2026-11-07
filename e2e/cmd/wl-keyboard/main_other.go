//go:build !linux || !cgo

// Command wl-keyboard is Linux-only (cgo, wayland-client, xkbcommon); see main.go.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "wl-keyboard needs Linux with cgo")
	os.Exit(2)
}
