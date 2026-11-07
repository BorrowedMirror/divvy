//go:build linux && cgo

package main

/*
#cgo pkg-config: wayland-client xkbcommon
#include "wayland.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// runWayland runs the game as a native Wayland client (xdg_toplevel); see wayland.h.
func runWayland(g *game, w, h uint32) error {
	if w > 1<<15 || h > 1<<15 {
		return fmt.Errorf("size %dx%d is too large", w, h)
	}
	ctitle, capp := C.CString(title), C.CString(appName)
	defer C.free(unsafe.Pointer(ctitle))
	defer C.free(unsafe.Pointer(capp))
	wl := C.fg_open(C.int(w), C.int(h), ctitle, capp, C.uint32_t(g.color()))
	if wl == nil {
		return fmt.Errorf("wayland: %s", C.GoString(C.fg_open_error()))
	}
	for {
		var k C.fg_key
		switch C.fg_next(wl, &k) {
		case C.FG_READY:
			g.drawn()
		case C.FG_KEY:
			ev := keyEvent{
				pressed: k.pressed != 0,
				isC:     k.is_c != 0,
				isV:     k.is_v != 0,
				code:    keysymName(uint32(k.keysym), uint32(k.key)+8),
				mods:    mods{ctrl: k.ctrl != 0, alt: k.alt != 0, shift: k.shift != 0, super: k.super != 0},
			}
			if g.key(ev, wlClipboard{wl, k.serial}) {
				wl.color = C.uint32_t(g.color())
				if C.fg_draw(wl) != 0 {
					return errors.New("wayland: drawing failed")
				}
			}
		case C.FG_CLOSED:
			return nil
		default:
			return errors.New("wayland: connection lost")
		}
	}
}

// wlClipboard is the Wayland selection as of one key event (its serial authorizes the change).
type wlClipboard struct {
	wl     *C.fg_wl
	serial C.uint32_t
}

func (cb wlClipboard) setText(text string) error {
	ct := C.CString(text)
	defer C.free(unsafe.Pointer(ct))
	if C.fg_set_selection(cb.wl, ct, C.size_t(len(text)), cb.serial) != 0 {
		return errors.New("wayland: setting the selection failed")
	}
	return nil
}

func (cb wlClipboard) text() (string, error) {
	if cb.wl.source != nil { // ours: reading it here would wait on ourselves
		return C.GoStringN(cb.wl.text, C.int(cb.wl.text_len)), nil
	}
	t := C.fg_read_selection(cb.wl)
	if t.err != 0 {
		return "", errors.New("wayland: reading the selection failed")
	}
	defer C.free(unsafe.Pointer(t.buf))
	return C.GoStringN(t.buf, C.int(t.len)), nil
}
