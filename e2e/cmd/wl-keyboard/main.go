//go:build linux && cgo

// Command wl-keyboard keeps a zwp_virtual_keyboard_v1 with a full xkb keymap alive on a
// headless wlroots compositor, so its seat has a keyboard (and a real keymap) like any desktop
// session. Used by e2e/scripts/run-wayland.sh.
//
// Headless sway has no input devices: its seat sends clients a `no_keymap` keymap and Xwayland's
// core keyboard has no modifier map, so XTest chords (xdotool) never reach X clients. The keymap
// comes from the XKB_DEFAULT_* environment, else "us" (the app's own virtual keyboard code,
// internal/platform/wayland/vkbd.h).
//
//	wl-keyboard        prints READY once the keymap is uploaded, then blocks until killed
package main

/*
#cgo pkg-config: wayland-client xkbcommon
#cgo CFLAGS: -I${SRCDIR}/../../../internal/platform/wayland
#include "vkbd.h"
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// C memory: the registry listener keeps a pointer to it, which Go memory must not hand out.
	// It is never freed: it lives until the process exits.
	vk := (*C.rkl_vk)(C.calloc(1, C.sizeof_rkl_vk))
	if rc := C.rkl_vk_open(vk, nil); rc != C.RKL_VK_OK {
		fmt.Fprintf(os.Stderr, "wl-keyboard: cannot create a virtual keyboard on $WAYLAND_DISPLAY (code %d)\n", int(rc))
		os.Exit(1)
	}
	fmt.Println("READY")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	lost := make(chan struct{})
	go func() {
		// Keep reading the connection so the compositor never sees a stalled client.
		for C.wl_display_dispatch(vk.display) >= 0 {
		}
		close(lost)
	}()
	select {
	case <-ctx.Done():
	case <-lost:
		fmt.Fprintln(os.Stderr, "wl-keyboard: lost the Wayland connection")
		os.Exit(1)
	}
	stop()
}
