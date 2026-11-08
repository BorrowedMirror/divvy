//go:build linux && !cgo

package x11

import (
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// xidFromGtk needs cgo (GTK/GDK calls).
func xidFromGtk(unsafe.Pointer) (uint32, error) {
	return 0, platform.Unsupported("overlay: built without cgo")
}

// gtkRefuseFocus needs cgo; without it the window keeps the focus hints Prepare sets.
func gtkRefuseFocus(unsafe.Pointer) {}

// gtkToolWindow needs cgo.
func gtkToolWindow(unsafe.Pointer, string) {}
