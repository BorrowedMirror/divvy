package win

import (
	"unsafe"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// Win32 extended window styles we touch (values from WinUser.h).
const (
	wsExTopmost     uint32 = 0x0000_0008
	wsExTransparent uint32 = 0x0000_0020
	wsExToolWindow  uint32 = 0x0000_0080
	wsExLayered     uint32 = 0x0008_0000
	wsExNoActivate  uint32 = 0x0800_0000
)

// preparedExStyle is the overlay's extended style after Prepare: never activates, no taskbar
// button, topmost.
func preparedExStyle(current uint32) uint32 {
	return current | wsExNoActivate | wsExToolWindow | wsExTopmost
}

// clickThroughExStyle is the extended style after toggling click-through. weAddedLayered says
// whether WS_EX_LAYERED was added by us earlier (so we only remove what we added). It returns
// the new style and the new weAddedLayered flag.
func clickThroughExStyle(current uint32, enabled, weAddedLayered bool) (uint32, bool) {
	if enabled {
		added := weAddedLayered || current&wsExLayered == 0
		return current | wsExLayered | wsExTransparent, added
	}
	style := current &^ wsExTransparent
	if weAddedLayered {
		style &^= wsExLayered
	}
	return style, false
}

// hwndFrom reads the HWND out of a Wails NativeWindow() pointer, which on Windows is the HWND
// value itself (not a pointer to one). A nil pointer is not a Win32 window.
func hwndFrom(native unsafe.Pointer) (uintptr, error) {
	if native == nil {
		return 0, platform.Unsupported("overlay: expected a Win32 window handle")
	}
	return uintptr(native), nil
}
