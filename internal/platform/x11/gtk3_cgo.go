//go:build linux && cgo && gtk3

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

import "unsafe"

// gtkXID is the X window id of a GtkWindow*, 0 when GDK is not on X11.
func gtkXID(native unsafe.Pointer) uint32 { return uint32(C.rkl_gtk_xid(native)) }
