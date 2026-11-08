//go:build linux && cgo

package x11

import (
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// xidFromGtk is the X window id of a GtkWindow* (a Wails window's NativeWindow()). It calls
// GTK, so it must run on the GTK main thread (inside application.InvokeSync).
func xidFromGtk(native unsafe.Pointer) (uint32, error) {
	if native == nil {
		return 0, platform.Unsupported("overlay: no native window")
	}
	xid := gtkXID(native)
	if xid == 0 {
		return 0, platform.Unsupported("overlay: the GTK window is not an X11 window (GDK_BACKEND must be x11)")
	}
	return xid, nil
}
