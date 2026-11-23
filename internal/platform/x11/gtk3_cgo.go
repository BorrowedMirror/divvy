//go:build linux && cgo && gtk3

package x11

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include <gtk/gtk.h>
#include <gdk/gdkx.h>

// divvy_gtk_xid returns the X window id of a GtkWindow*, realising it first when needed (Wails
// creates windows hidden; GTK only realises on first show). 0 when GDK is not on X11.
static unsigned long divvy_gtk_xid(void *native) {
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

// divvy_gtk_refuse_focus keeps a GtkWindow* from ever taking keyboard focus: GDK writes the
// WM_HINTS input field from accept-focus on every show, and focus-on-map off sets a zero
// user time so window managers do not focus it when it is mapped.
static void divvy_gtk_refuse_focus(void *native) {
	GtkWindow *window = GTK_WINDOW(native);
	gtk_window_set_accept_focus(window, FALSE);
	gtk_window_set_focus_on_map(window, FALSE);
}

// divvy_gtk_tool_window titles a GtkWindow* and keeps it out of taskbars and pagers.
static void divvy_gtk_tool_window(void *native, const char *title) {
	GtkWindow *window = GTK_WINDOW(native);
	gtk_window_set_title(window, title);
	gtk_window_set_skip_taskbar_hint(window, TRUE);
	gtk_window_set_skip_pager_hint(window, TRUE);
}
*/
import "C"

import "unsafe"

// gtkXID is the X window id of a GtkWindow*, 0 when GDK is not on X11.
func gtkXID(native unsafe.Pointer) uint32 { return uint32(C.divvy_gtk_xid(native)) }

// gtkRefuseFocus keeps a GtkWindow* from taking keyboard focus. GTK main thread only.
func gtkRefuseFocus(native unsafe.Pointer) { C.divvy_gtk_refuse_focus(native) }

// gtkToolWindow titles a GtkWindow* and keeps it out of taskbars and pagers. GTK main thread
// only.
func gtkToolWindow(native unsafe.Pointer, title string) {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	C.divvy_gtk_tool_window(native, cTitle)
}
