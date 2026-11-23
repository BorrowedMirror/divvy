//go:build linux && cgo

package wayland

/*
#cgo pkg-config: wayland-client xkbcommon
#include "vkbd.h"
*/
import "C"

import (
	"unsafe"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// vkConn is an open virtual keyboard. Its state lives in C memory: the registry listener keeps
// a pointer to it, which Go memory must not hand out.
type vkConn struct {
	c *C.divvy_vk
}

// openVirtualKeyboard connects to display ("" = $WAYLAND_DISPLAY) and creates a virtual
// keyboard there.
func openVirtualKeyboard(display string) (*vkConn, error) {
	var name *C.char
	if display != "" {
		name = C.CString(display)
		defer C.free(unsafe.Pointer(name))
	}
	vk := (*C.divvy_vk)(C.calloc(1, C.sizeof_divvy_vk))
	if rc := C.divvy_vk_open(vk, name); rc != C.DIVVY_VK_OK {
		C.free(unsafe.Pointer(vk))
		return nil, platform.Backend("virtual keyboard: %s", vkError(int(rc)))
	}
	return &vkConn{c: vk}, nil
}

func vkError(rc int) string {
	switch rc {
	case C.DIVVY_VK_CONNECT:
		return "cannot connect to the Wayland display"
	case C.DIVVY_VK_NO_SEAT:
		return "compositor has no seat"
	case C.DIVVY_VK_NO_MANAGER:
		return "compositor does not implement zwp_virtual_keyboard_manager_v1"
	case C.DIVVY_VK_KEYMAP:
		return "cannot build the xkb keymap"
	default:
		return "Wayland protocol error"
	}
}

func (v *vkConn) key(code uint16, down bool) error {
	d := C.int(0)
	if down {
		d = 1
	}
	if C.divvy_vk_key(v.c, C.uint32_t(code), d) != 0 {
		return platform.Backend("virtual keyboard: connection lost")
	}
	return nil
}

func (v *vkConn) sync() error {
	if C.divvy_vk_sync(v.c) != 0 {
		return platform.Backend("virtual keyboard: connection lost")
	}
	return nil
}

func (v *vkConn) close() {
	C.divvy_vk_close(v.c)
	C.free(unsafe.Pointer(v.c))
	v.c = nil
}
