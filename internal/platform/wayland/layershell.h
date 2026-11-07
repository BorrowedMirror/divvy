// gtk-layer-shell (GTK3) helpers for layershell_cgo.go. GTK main thread only.
#ifndef RKL_LAYERSHELL_H
#define RKL_LAYERSHELL_H

#include <gtk/gtk.h>
#include <gtk-layer-shell.h>

enum {
	RKL_LS_OK = 0,
	RKL_LS_UNSUPPORTED = 1, // compositor lacks wlr-layer-shell (or GDK is not on Wayland)
	RKL_LS_UNREALISED = 2,  // OK, but the window was realised and had to be unrealised first
	RKL_LS_NOT_LAYER = 3,   // not a layer surface
};

// rkl_ls_prepare applies the overlay's window hints and, when layer is set, makes it a
// layer-shell surface on the OVERLAY layer anchored top-right. gtk-layer-shell must be set up
// before the window is realised; a realised window is hidden and unrealised first.
static int rkl_ls_prepare(GtkWindow *w, int layer) {
	gtk_window_set_keep_above(w, TRUE);
	gtk_window_set_skip_taskbar_hint(w, TRUE);
	gtk_window_set_skip_pager_hint(w, TRUE);
	gtk_window_stick(w);
	if (!layer) return RKL_LS_OK;
	if (!gtk_layer_is_supported()) return RKL_LS_UNSUPPORTED;
	if (gtk_layer_is_layer_window(w)) return RKL_LS_OK;
	int rc = RKL_LS_OK;
	if (gtk_widget_get_realized(GTK_WIDGET(w))) {
		gtk_widget_hide(GTK_WIDGET(w));
		gtk_widget_unrealize(GTK_WIDGET(w));
		rc = RKL_LS_UNREALISED;
	}
	gtk_layer_init_for_window(w);
	gtk_layer_set_layer(w, GTK_LAYER_SHELL_LAYER_OVERLAY);
	gtk_layer_set_namespace(w, "rkl-overlay");
	// Keyboard only when the user clicks into the overlay (filter editing); never steal it.
	gtk_layer_set_keyboard_mode(w, GTK_LAYER_SHELL_KEYBOARD_MODE_ON_DEMAND);
	gtk_layer_set_exclusive_zone(w, -1);
	gtk_layer_set_anchor(w, GTK_LAYER_SHELL_EDGE_TOP, TRUE);
	gtk_layer_set_anchor(w, GTK_LAYER_SHELL_EDGE_RIGHT, TRUE);
	gtk_layer_set_margin(w, GTK_LAYER_SHELL_EDGE_TOP, 48);
	gtk_layer_set_margin(w, GTK_LAYER_SHELL_EDGE_RIGHT, 24);
	// gtk-layer-shell sizes the surface from the window's size request, not its default size,
	// and a web view requests next to nothing: the surface would be configured but never
	// painted. Pin the request to the size Wails gave the window.
	int width = 0, height = 0;
	gtk_window_get_size(w, &width, &height);
	if (width > 0 && height > 0) gtk_widget_set_size_request(GTK_WIDGET(w), width, height);
	return rc;
}

static int rkl_ls_is_layer(GtkWindow *w) { return gtk_layer_is_layer_window(w); }

// rkl_ls_anchor places the layer surface against the right (or left) output edge.
static int rkl_ls_anchor(GtkWindow *w, int right, int top, int side) {
	if (!gtk_layer_is_layer_window(w)) return RKL_LS_NOT_LAYER;
	gtk_layer_set_anchor(w, GTK_LAYER_SHELL_EDGE_TOP, TRUE);
	gtk_layer_set_anchor(w, GTK_LAYER_SHELL_EDGE_RIGHT, right);
	gtk_layer_set_anchor(w, GTK_LAYER_SHELL_EDGE_LEFT, !right);
	gtk_layer_set_margin(w, GTK_LAYER_SHELL_EDGE_TOP, top);
	gtk_layer_set_margin(w, right ? GTK_LAYER_SHELL_EDGE_RIGHT : GTK_LAYER_SHELL_EDGE_LEFT, side);
	return RKL_LS_OK;
}

// rkl_ls_monitor_area is the geometry (GDK logical px) of the monitor at (x, y); a layer
// surface is moved to that monitor.
static GdkRectangle rkl_ls_monitor_area(GtkWindow *w, int x, int y) {
	GdkRectangle area = {0, 0, 0, 0};
	GdkMonitor *m = gdk_display_get_monitor_at_point(gtk_widget_get_display(GTK_WIDGET(w)), x, y);
	if (!m) return area;
	gdk_monitor_get_geometry(m, &area);
	if (gtk_layer_is_layer_window(w) && gtk_layer_get_monitor(w) != m) gtk_layer_set_monitor(w, m);
	return area;
}

// rkl_ls_click_through empties (or resets) the input region; a no-op until realised.
static void rkl_ls_click_through(GtkWindow *w, int on) {
	GdkWindow *g = gtk_widget_get_window(GTK_WIDGET(w));
	if (!g) return;
	if (on) {
		cairo_region_t *empty = cairo_region_create();
		gdk_window_input_shape_combine_region(g, empty, 0, 0);
		cairo_region_destroy(empty);
	} else {
		gdk_window_input_shape_combine_region(g, NULL, 0, 0);
	}
}

static void rkl_ls_show(GtkWindow *w, int x, int y, int width, int height) {
	int layer = gtk_layer_is_layer_window(w);
	if (width > 0 && height > 0) {
		// A layer surface's size is its size request (see rkl_ls_prepare).
		if (layer) gtk_widget_set_size_request(GTK_WIDGET(w), width, height);
		gtk_window_resize(w, width, height);
	}
	if (!layer) gtk_window_move(w, x, y);
	gtk_widget_show_all(GTK_WIDGET(w));
}

// rkl_ls_resize sizes a layer surface (gtk_window_resize alone does not).
static void rkl_ls_resize(GtkWindow *w, int width, int height) {
	if (gtk_layer_is_layer_window(w)) gtk_widget_set_size_request(GTK_WIDGET(w), width, height);
	gtk_window_resize(w, width, height);
}

static void rkl_ls_hide(GtkWindow *w) { gtk_widget_hide(GTK_WIDGET(w)); }

static int rkl_ls_mapped(GtkWindow *w) { return gtk_widget_get_mapped(GTK_WIDGET(w)); }

#endif
