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
