//go:build linux && cgo && !gtk3

package x11

/*
#cgo pkg-config: gtk4 gtk4-x11
// GTK4 deprecates its X11 calls; the X11 backend still needs them (Wails uses the same one).
#cgo CFLAGS: -Wno-deprecated-declarations
#include <gtk/gtk.h>
#include <gdk/x11/gdkx.h>

// rkl_gtk_xid returns the X window id of a GtkWindow* in a GTK4 build (Wails' default without
// -tags gtk3), realising it first when needed. 0 when GDK is not on X11.
static unsigned long rkl_gtk_xid(void *native) {
	GtkWidget *widget = GTK_WIDGET(native);
	if (!gtk_widget_get_realized(widget)) {
		gtk_widget_realize(widget);
	}
	GdkSurface *surface = gtk_native_get_surface(GTK_NATIVE(widget));
	if (surface == NULL || !GDK_IS_X11_SURFACE(surface)) {
		return 0;
	}
	return (unsigned long)gdk_x11_surface_get_xid(surface);
}
*/
import "C"

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
	xid := uint32(C.rkl_gtk_xid(native))
	if xid == 0 {
		return 0, platform.Unsupported("overlay: the GTK window is not an X11 window (GDK_BACKEND must be x11)")
	}
	return xid, nil
}
