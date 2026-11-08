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

import "unsafe"

// gtkXID is the X window id of a GtkWindow*, 0 when GDK is not on X11.
func gtkXID(native unsafe.Pointer) uint32 { return uint32(C.rkl_gtk_xid(native)) }
