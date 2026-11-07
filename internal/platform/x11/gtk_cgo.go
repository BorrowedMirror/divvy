//go:build linux && cgo

package x11

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <gdk/gdkx.h>

// rkl_gtk_xid returns the X window id of a GtkWindow*, realising it first when needed (Wails
// creates windows hidden; GTK only realises on first show). 0 when GDK is not on X11.
static unsigned long rkl_gtk_xid(void *native) {
	GtkWidget *widget = GTK_WIDGET(native);
	if (!gtk_widget_get_realized(widget)) {
		gtk_widget_realize(widget);
	}
	GdkWindow *gdk = gtk_widget_get_window(widget);
	if (gdk == NULL || !GDK_IS_X11_WINDOW(gdk)) {
		return 0;
	}
	return (unsigned long)gdk_x11_window_get_xid(gdk);
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
