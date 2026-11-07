package win

import (
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// hwndFrom reads the HWND out of a Wails NativeWindow() pointer, which on Windows is the HWND
// value itself (not a pointer to one). A nil pointer is not a Win32 window.
func hwndFrom(native unsafe.Pointer) (uintptr, error) {
	if native == nil {
		return 0, platform.Unsupported("overlay: expected a Win32 window handle")
	}
	return uintptr(native), nil
}
