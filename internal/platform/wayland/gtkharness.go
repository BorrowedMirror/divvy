//go:build linux && cgo && gtk3 && waylandtest

package wayland

// GTK helpers for the layer-shell integration test (cgo is not allowed in _test.go files).

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include <gtk/gtk.h>

static int rkl_test_gtk_init(void) { return gtk_init_check(NULL, NULL); }

static GtkWindow *rkl_test_window(const char *title) {
	GtkWidget *w = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(w), title);
	gtk_window_set_default_size(GTK_WINDOW(w), 200, 100);
	gtk_container_add(GTK_CONTAINER(w), gtk_label_new(title));
	return GTK_WINDOW(w);
}

static void rkl_test_iterate(void) {
	while (gtk_events_pending()) gtk_main_iteration_do(FALSE);
}

static void rkl_test_show(GtkWindow *w) {
	gtk_widget_realize(GTK_WIDGET(w));
	gtk_widget_show_all(GTK_WIDGET(w));
}

static guint rkl_test_last_keyval;
static guint rkl_test_last_state;

static gboolean rkl_test_on_key(GtkWidget *w, GdkEventKey *e, gpointer data) {
	(void)w, (void)data;
	rkl_test_last_keyval = e->keyval;
	rkl_test_last_state = e->state & (GDK_CONTROL_MASK | GDK_SHIFT_MASK | GDK_MOD1_MASK);
	return FALSE;
}

static void rkl_test_watch_keys(GtkWindow *w) {
	g_signal_connect(w, "key-press-event", G_CALLBACK(rkl_test_on_key), NULL);
}

static guint rkl_test_keyval(void) { return rkl_test_last_keyval; }
static guint rkl_test_state(void) { return rkl_test_last_state; }

static int rkl_test_realized(GtkWindow *w) { return gtk_widget_get_realized(GTK_WIDGET(w)); }
static int rkl_test_has_gdk_window(GtkWindow *w) { return gtk_widget_get_window(GTK_WIDGET(w)) != NULL; }
*/
import "C"

import (
	"unsafe"
)

func gtkTestInit() bool { return C.rkl_test_gtk_init() != 0 }

func gtkTestWindow(title string) unsafe.Pointer {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	return unsafe.Pointer(C.rkl_test_window(t))
}

func gtkTestIterate() { C.rkl_test_iterate() }

// gtkTestShow realises and shows a window the way Wails' show does.
func gtkTestShow(w unsafe.Pointer) { C.rkl_test_show((*C.GtkWindow)(w)) }

func gtkTestRealized(w unsafe.Pointer) bool { return C.rkl_test_realized((*C.GtkWindow)(w)) != 0 }

func gtkTestHasGdkWindow(w unsafe.Pointer) bool {
	return C.rkl_test_has_gdk_window((*C.GtkWindow)(w)) != 0
}

// gtkTestWatchKeys records the last key press on w.
func gtkTestWatchKeys(w unsafe.Pointer) { C.rkl_test_watch_keys((*C.GtkWindow)(w)) }

// gtkTestLastKey is the last key press: its keysym name and whether Ctrl/Shift/Alt were down.
func gtkTestLastKey() (name string, ctrl, shift, alt bool) {
	kv := C.rkl_test_keyval()
	if kv == 0 {
		return "", false, false, false
	}
	st := C.rkl_test_state()
	return C.GoString(C.gdk_keyval_name(kv)),
		st&C.GDK_CONTROL_MASK != 0, st&C.GDK_SHIFT_MASK != 0, st&C.GDK_MOD1_MASK != 0
}
