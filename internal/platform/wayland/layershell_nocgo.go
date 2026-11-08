//go:build linux && !(cgo && gtk3)

package wayland

import (
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// newOverlay needs cgo and the gtk3 build tag (the Wails GTK3 build) to drive gtk-layer-shell.
func newOverlay(unsafe.Pointer, func(func()), bool) (platform.OverlayWindow, error) {
	return nil, platform.Unsupported("wayland overlay needs gtk-layer-shell (build with cgo and -tags gtk3)")
}
