// Native Wayland window for fake-game (wayland_linux.go): an xdg_toplevel with a solid wl_shm
// buffer, a wl_keyboard (xkbcommon keymap) and a wl_data_source for the clipboard.
//
// The xdg-shell tables below are the subset of what `wayland-scanner private-code` generates
// for xdg-shell.xml (xdg_wm_base, xdg_surface, xdg_toplevel at version 1, up to the last
// request used), made static so this header can live in a cgo preamble:
//
//   Copyright © 2008-2013 Kristian Høgsberg
//   Copyright © 2013      Rafael Antognolli
//   Copyright © 2013      Jasper St. Pierre
//   Copyright © 2010-2013 Intel Corporation
//   Copyright © 2015-2017 Samsung Electronics Co., Ltd
//   Copyright © 2015-2017 Red Hat Inc.
//
//   Permission is hereby granted, free of charge, to any person obtaining a copy of this
//   software and associated documentation files (the "Software"), to deal in the Software
//   without restriction, including without limitation the rights to use, copy, modify, merge,
//   publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons
//   to whom the Software is furnished to do so, subject to the following conditions: The above
//   copyright notice and this permission notice (including the next paragraph) shall be
//   included in all copies or substantial portions of the Software. THE SOFTWARE IS PROVIDED
//   "AS IS", WITHOUT WARRANTY OF ANY KIND.
#ifndef FG_WAYLAND_H
#define FG_WAYLAND_H

#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <linux/input-event-codes.h>
#include <poll.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>
#include <wayland-client.h>
#include <xkbcommon/xkbcommon.h>

// --- xdg-shell (subset) ---------------------------------------------------------------------

static const struct wl_interface fg_xdg_surface_interface;
static const struct wl_interface fg_xdg_toplevel_interface;

static const struct wl_interface *fg_xdg_types[] = {
	NULL, NULL, NULL, NULL,
	&fg_xdg_surface_interface, &wl_surface_interface, // get_xdg_surface
	&fg_xdg_toplevel_interface,                        // get_toplevel
};

static const struct wl_message fg_xdg_wm_base_requests[] = {
	{"destroy", "", fg_xdg_types + 0},
	{"create_positioner", "n", fg_xdg_types + 0}, // unused
	{"get_xdg_surface", "no", fg_xdg_types + 4},
	{"pong", "u", fg_xdg_types + 0},
};
static const struct wl_message fg_xdg_wm_base_events[] = {
	{"ping", "u", fg_xdg_types + 0},
};
static const struct wl_interface fg_xdg_wm_base_interface = {
	"xdg_wm_base", 1, 4, fg_xdg_wm_base_requests, 1, fg_xdg_wm_base_events,
};

static const struct wl_message fg_xdg_surface_requests[] = {
	{"destroy", "", fg_xdg_types + 0},
	{"get_toplevel", "n", fg_xdg_types + 6},
	{"get_popup", "n?oo", fg_xdg_types + 0}, // unused
	{"set_window_geometry", "iiii", fg_xdg_types + 0},
	{"ack_configure", "u", fg_xdg_types + 0},
};
static const struct wl_message fg_xdg_surface_events[] = {
	{"configure", "u", fg_xdg_types + 0},
};
static const struct wl_interface fg_xdg_surface_interface = {
	"xdg_surface", 1, 5, fg_xdg_surface_requests, 1, fg_xdg_surface_events,
};

static const struct wl_message fg_xdg_toplevel_requests[] = {
	{"destroy", "", fg_xdg_types + 0},
	{"set_parent", "?o", fg_xdg_types + 0}, // unused
	{"set_title", "s", fg_xdg_types + 0},
	{"set_app_id", "s", fg_xdg_types + 0},
};
static const struct wl_message fg_xdg_toplevel_events[] = {
	{"configure", "iia", fg_xdg_types + 0},
	{"close", "", fg_xdg_types + 0},
};
static const struct wl_interface fg_xdg_toplevel_interface = {
	"xdg_toplevel", 1, 4, fg_xdg_toplevel_requests, 2, fg_xdg_toplevel_events,
};

struct fg_xdg_wm_base_listener {
	void (*ping)(void *data, struct wl_proxy *base, uint32_t serial);
};
struct fg_xdg_surface_listener {
	void (*configure)(void *data, struct wl_proxy *surface, uint32_t serial);
};
struct fg_xdg_toplevel_listener {
	void (*configure)(void *data, struct wl_proxy *toplevel, int32_t w, int32_t h, struct wl_array *states);
	void (*close)(void *data, struct wl_proxy *toplevel);
};

// --- state ----------------------------------------------------------------------------------

enum { FG_ERROR = -1, FG_CLOSED = 0, FG_KEY = 1, FG_READY = 2 };

// fg_key is one key press or release for Go.
typedef struct {
	int pressed;
	int is_c, is_v;
	int ctrl, alt, shift, super;
	uint32_t serial;
	uint32_t keysym;
	uint32_t key; // evdev code
} fg_key;

#define FG_QUEUE 64

typedef struct {
	struct wl_display *display;
	struct wl_registry *registry;
	struct wl_compositor *compositor;
	struct wl_shm *shm;
	struct wl_seat *seat;
	struct wl_keyboard *keyboard;
	struct wl_data_device_manager *ddm;
	struct wl_data_device *device;
	struct wl_proxy *wm_base, *xdg_surface, *toplevel;
	struct wl_surface *surface;

	int width, height;          // current buffer size
	int want_width, want_height; // from the last toplevel configure (0: our choice)
	int drawn, ready_pending, closed;
	uint32_t color;

	struct xkb_context *xkb;
	struct xkb_keymap *keymap;
	struct xkb_state *state;

	struct wl_data_source *source; // ours, while we own the selection
	char *text;
	size_t text_len;
	struct wl_data_offer *offer; // current selection

	fg_key queue[FG_QUEUE];
	int head, tail;
} fg_wl;

// --- drawing --------------------------------------------------------------------------------

static void fg_buffer_release(void *data, struct wl_buffer *buffer) {
	(void)data;
	wl_buffer_destroy(buffer);
}
static const struct wl_buffer_listener fg_buffer_listener = {fg_buffer_release};

// fg_draw commits a new buffer filled with w->color (0xRRGGBB).
static int fg_draw(fg_wl *w) {
	int width = w->width, height = w->height;
	size_t stride = (size_t)width * 4, size = stride * (size_t)height;
	int fd = memfd_create("fake-game", MFD_CLOEXEC);
	if (fd < 0) return -1;
	if (ftruncate(fd, (off_t)size) < 0) { close(fd); return -1; }
	uint32_t *px = mmap(NULL, size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
	if (px == MAP_FAILED) { close(fd); return -1; }
	for (size_t i = 0; i < size / 4; i++) px[i] = 0xff000000u | w->color;
	munmap(px, size);
	struct wl_shm_pool *pool = wl_shm_create_pool(w->shm, fd, (int32_t)size);
	struct wl_buffer *buf = wl_shm_pool_create_buffer(pool, 0, width, height, (int32_t)stride, WL_SHM_FORMAT_XRGB8888);
	wl_shm_pool_destroy(pool);
	close(fd);
	wl_buffer_add_listener(buf, &fg_buffer_listener, NULL);
	wl_surface_attach(w->surface, buf, 0, 0);
	wl_surface_damage(w->surface, 0, 0, width, height);
	wl_surface_commit(w->surface);
	if (!w->drawn) {
		w->drawn = 1;
		w->ready_pending = 1;
	}
	return wl_display_flush(w->display) < 0 && errno != EAGAIN ? -1 : 0;
}

// --- xdg-shell listeners --------------------------------------------------------------------

static void fg_ping(void *data, struct wl_proxy *base, uint32_t serial) {
	(void)data;
	wl_proxy_marshal_flags(base, 3, NULL, wl_proxy_get_version(base), 0, serial);
}
static const struct fg_xdg_wm_base_listener fg_wm_base_listener = {fg_ping};

static void fg_xdg_configure(void *data, struct wl_proxy *xs, uint32_t serial) {
	fg_wl *w = data;
	wl_proxy_marshal_flags(xs, 4, NULL, wl_proxy_get_version(xs), 0, serial); // ack_configure
	int width = w->want_width > 0 ? w->want_width : w->width;
	int height = w->want_height > 0 ? w->want_height : w->height;
	if (!w->drawn || width != w->width || height != w->height) {
		w->width = width;
		w->height = height;
		fg_draw(w);
	}
}
static const struct fg_xdg_surface_listener fg_xdg_surface_listener = {fg_xdg_configure};

static void fg_toplevel_configure(void *data, struct wl_proxy *t, int32_t width, int32_t height, struct wl_array *states) {
	fg_wl *w = data;
	(void)t, (void)states;
	w->want_width = width;
	w->want_height = height;
}
static void fg_toplevel_close(void *data, struct wl_proxy *t) {
	(void)t;
	((fg_wl *)data)->closed = 1;
}
static const struct fg_xdg_toplevel_listener fg_toplevel_listener = {fg_toplevel_configure, fg_toplevel_close};

// --- keyboard -------------------------------------------------------------------------------

static void fg_kb_keymap(void *data, struct wl_keyboard *kb, uint32_t format, int32_t fd, uint32_t size) {
	fg_wl *w = data;
	(void)kb;
	if (format != WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1) { close(fd); return; }
	char *map = mmap(NULL, size, PROT_READ, MAP_PRIVATE, fd, 0);
	close(fd);
	if (map == MAP_FAILED) return;
	struct xkb_keymap *km = xkb_keymap_new_from_string(w->xkb, map, XKB_KEYMAP_FORMAT_TEXT_V1, XKB_KEYMAP_COMPILE_NO_FLAGS);
	munmap(map, size);
	if (!km) return;
	struct xkb_state *st = xkb_state_new(km);
	if (!st) { xkb_keymap_unref(km); return; }
	if (w->state) xkb_state_unref(w->state);
	if (w->keymap) xkb_keymap_unref(w->keymap);
	w->keymap = km;
	w->state = st;
}

static void fg_kb_enter(void *data, struct wl_keyboard *kb, uint32_t serial, struct wl_surface *s, struct wl_array *keys) {
	(void)data, (void)kb, (void)serial, (void)s, (void)keys;
}
static void fg_kb_leave(void *data, struct wl_keyboard *kb, uint32_t serial, struct wl_surface *s) {
	(void)data, (void)kb, (void)serial, (void)s;
}

static int fg_mod(fg_wl *w, const char *name) {
	return xkb_state_mod_name_is_active(w->state, name, XKB_STATE_MODS_EFFECTIVE) > 0;
}

static void fg_kb_key(void *data, struct wl_keyboard *kb, uint32_t serial, uint32_t time, uint32_t key, uint32_t state) {
	fg_wl *w = data;
	(void)kb, (void)time;
	if (state != WL_KEYBOARD_KEY_STATE_PRESSED && state != WL_KEYBOARD_KEY_STATE_RELEASED) return;
	int next = (w->tail + 1) % FG_QUEUE;
	if (next == w->head) return; // Go is not keeping up; drop
	fg_key *k = &w->queue[w->tail];
	memset(k, 0, sizeof *k);
	k->pressed = state == WL_KEYBOARD_KEY_STATE_PRESSED;
	k->serial = serial;
	k->key = key;
	if (w->state) {
		k->keysym = xkb_state_key_get_one_sym(w->state, key + 8);
		k->ctrl = fg_mod(w, XKB_MOD_NAME_CTRL);
		k->alt = fg_mod(w, XKB_MOD_NAME_ALT);
		k->shift = fg_mod(w, XKB_MOD_NAME_SHIFT);
		k->super = fg_mod(w, XKB_MOD_NAME_LOGO);
	}
	uint32_t lower = xkb_keysym_to_lower(k->keysym);
	k->is_c = key == KEY_C || lower == XKB_KEY_c;
	k->is_v = key == KEY_V || lower == XKB_KEY_v;
	w->tail = next;
}

static void fg_kb_modifiers(void *data, struct wl_keyboard *kb, uint32_t serial, uint32_t dep, uint32_t lat, uint32_t lock, uint32_t group) {
	fg_wl *w = data;
	(void)kb, (void)serial;
	if (w->state) xkb_state_update_mask(w->state, dep, lat, lock, 0, 0, group);
}

static void fg_kb_repeat(void *data, struct wl_keyboard *kb, int32_t rate, int32_t delay) {
	(void)data, (void)kb, (void)rate, (void)delay;
}

static const struct wl_keyboard_listener fg_kb_listener = {
	fg_kb_keymap, fg_kb_enter, fg_kb_leave, fg_kb_key, fg_kb_modifiers, fg_kb_repeat,
};

static void fg_seat_caps(void *data, struct wl_seat *seat, uint32_t caps) {
	fg_wl *w = data;
	if ((caps & WL_SEAT_CAPABILITY_KEYBOARD) && !w->keyboard) {
		w->keyboard = wl_seat_get_keyboard(seat);
		wl_keyboard_add_listener(w->keyboard, &fg_kb_listener, w);
	} else if (!(caps & WL_SEAT_CAPABILITY_KEYBOARD) && w->keyboard) {
		wl_keyboard_release(w->keyboard);
		w->keyboard = NULL;
	}
}
static void fg_seat_name(void *data, struct wl_seat *seat, const char *name) {
	(void)data, (void)seat, (void)name;
}
static const struct wl_seat_listener fg_seat_listener = {fg_seat_caps, fg_seat_name};

// --- clipboard ------------------------------------------------------------------------------

static void fg_source_target(void *data, struct wl_data_source *s, const char *mime) {
	(void)data, (void)s, (void)mime;
}

static void fg_source_send(void *data, struct wl_data_source *s, const char *mime, int32_t fd) {
	fg_wl *w = data;
	(void)s, (void)mime;
	// Blocking writes: item texts are far smaller than a pipe buffer.
	int flags = fcntl(fd, F_GETFL);
	if (flags >= 0) fcntl(fd, F_SETFL, flags & ~O_NONBLOCK);
	size_t off = 0;
	while (off < w->text_len) {
		ssize_t n = write(fd, w->text + off, w->text_len - off);
		if (n < 0 && errno == EINTR) continue;
		if (n <= 0) break;
		off += (size_t)n;
	}
	close(fd);
}

static void fg_source_cancelled(void *data, struct wl_data_source *s) {
	fg_wl *w = data;
	if (w->source == s) w->source = NULL;
	wl_data_source_destroy(s);
}

static void fg_source_noop(void *data, struct wl_data_source *s) { (void)data, (void)s; }
static void fg_source_action(void *data, struct wl_data_source *s, uint32_t a) { (void)data, (void)s, (void)a; }

static const struct wl_data_source_listener fg_source_listener = {
	fg_source_target, fg_source_send, fg_source_cancelled, fg_source_noop, fg_source_noop, fg_source_action,
};

static void fg_dd_data_offer(void *data, struct wl_data_device *d, struct wl_data_offer *offer) {
	(void)data, (void)d, (void)offer;
}
static void fg_dd_enter(void *data, struct wl_data_device *d, uint32_t serial, struct wl_surface *s, wl_fixed_t x, wl_fixed_t y, struct wl_data_offer *o) {
	(void)data, (void)d, (void)serial, (void)s, (void)x, (void)y;
	if (o) wl_data_offer_destroy(o);
}
static void fg_dd_noop(void *data, struct wl_data_device *d) { (void)data, (void)d; }
static void fg_dd_motion(void *data, struct wl_data_device *d, uint32_t t, wl_fixed_t x, wl_fixed_t y) {
	(void)data, (void)d, (void)t, (void)x, (void)y;
}
static void fg_dd_selection(void *data, struct wl_data_device *d, struct wl_data_offer *offer) {
	fg_wl *w = data;
	(void)d;
	if (w->offer && w->offer != offer) wl_data_offer_destroy(w->offer);
	w->offer = offer;
}
static const struct wl_data_device_listener fg_dd_listener = {
	fg_dd_data_offer, fg_dd_enter, fg_dd_noop, fg_dd_motion, fg_dd_noop, fg_dd_selection,
};

// fg_set_selection owns the clipboard with text, as of the input event with serial.
static int fg_set_selection(fg_wl *w, const char *text, size_t len, uint32_t serial) {
	if (!w->device) return -1;
	char *copy = malloc(len + 1);
	if (!copy) return -1;
	memcpy(copy, text, len);
	copy[len] = 0;
	free(w->text);
	w->text = copy;
	w->text_len = len;
	struct wl_data_source *s = wl_data_device_manager_create_data_source(w->ddm);
	wl_data_source_add_listener(s, &fg_source_listener, w);
	static const char *mimes[] = {"text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "TEXT", "STRING"};
	for (size_t i = 0; i < sizeof mimes / sizeof *mimes; i++) wl_data_source_offer(s, mimes[i]);
	wl_data_device_set_selection(w->device, s, serial);
	w->source = s; // the previous one gets `cancelled`
	return wl_display_roundtrip(w->display) < 0 ? -1 : 0;
}

typedef struct {
	char *buf; // malloc'd, or NULL
	size_t len;
	int err;
} fg_text;

// fg_read_selection reads the current selection as text.
static fg_text fg_read_selection(fg_wl *w) {
	fg_text t = {NULL, 0, 0};
	if (!w->offer) return t;
	int fds[2];
	if (pipe2(fds, O_CLOEXEC) < 0) {
		t.err = 1;
		return t;
	}
	wl_data_offer_receive(w->offer, "text/plain;charset=utf-8", fds[1]);
	close(fds[1]);
	wl_display_flush(w->display);
	size_t cap = 4096, n = 0;
	char *buf = malloc(cap);
	for (;;) {
		struct pollfd p = {fds[0], POLLIN, 0};
		if (poll(&p, 1, 2000) <= 0) break;
		if (n == cap) {
			char *nb = realloc(buf, cap * 2);
			if (!nb) break;
			buf = nb;
			cap *= 2;
		}
		ssize_t r = read(fds[0], buf + n, cap - n);
		if (r < 0 && errno == EINTR) continue;
		if (r <= 0) break;
		n += (size_t)r;
	}
	close(fds[0]);
	t.buf = buf;
	t.len = n;
	return t;
}

// --- setup and loop -------------------------------------------------------------------------

static void fg_global(void *data, struct wl_registry *r, uint32_t name, const char *iface, uint32_t version) {
	fg_wl *w = data;
	if (!w->compositor && strcmp(iface, wl_compositor_interface.name) == 0) {
		w->compositor = wl_registry_bind(r, name, &wl_compositor_interface, version < 4 ? version : 4);
	} else if (!w->shm && strcmp(iface, wl_shm_interface.name) == 0) {
		w->shm = wl_registry_bind(r, name, &wl_shm_interface, 1);
	} else if (!w->seat && strcmp(iface, wl_seat_interface.name) == 0) {
		w->seat = wl_registry_bind(r, name, &wl_seat_interface, version < 5 ? version : 5);
		wl_seat_add_listener(w->seat, &fg_seat_listener, w);
	} else if (!w->ddm && strcmp(iface, wl_data_device_manager_interface.name) == 0) {
		w->ddm = wl_registry_bind(r, name, &wl_data_device_manager_interface, version < 3 ? version : 3);
	} else if (!w->wm_base && strcmp(iface, fg_xdg_wm_base_interface.name) == 0) {
		w->wm_base = wl_registry_bind(r, name, &fg_xdg_wm_base_interface, 1);
		wl_proxy_add_listener(w->wm_base, (void (**)(void)) & fg_wm_base_listener, w);
	}
}
static void fg_global_remove(void *data, struct wl_registry *r, uint32_t name) {
	(void)data, (void)r, (void)name;
}
static const struct wl_registry_listener fg_registry_listener = {fg_global, fg_global_remove};

// fg_open_error is why the last fg_open failed.
static const char *fg_open_error_msg = "";
static const char *fg_open_error(void) { return fg_open_error_msg; }

// fg_open connects to $WAYLAND_DISPLAY and creates the window. It returns NULL and sets
// fg_open_error on failure.
static fg_wl *fg_open(int width, int height, const char *title, const char *app_id, uint32_t color) {
	const char **err = &fg_open_error_msg;
	fg_wl *w = calloc(1, sizeof *w);
	if (!w) { *err = "out of memory"; return NULL; }
	w->width = width;
	w->height = height;
	w->color = color;
	w->xkb = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
	w->display = wl_display_connect(NULL);
	if (!w->display) { *err = "cannot connect to $WAYLAND_DISPLAY"; return NULL; }
	w->registry = wl_display_get_registry(w->display);
	wl_registry_add_listener(w->registry, &fg_registry_listener, w);
	if (wl_display_roundtrip(w->display) < 0) { *err = "registry roundtrip failed"; return NULL; }
	if (!w->compositor || !w->shm || !w->wm_base) { *err = "compositor lacks wl_compositor, wl_shm or xdg_wm_base"; return NULL; }
	if (!w->seat || !w->ddm) { *err = "compositor lacks wl_seat or wl_data_device_manager"; return NULL; }
	w->device = wl_data_device_manager_get_data_device(w->ddm, w->seat);
	wl_data_device_add_listener(w->device, &fg_dd_listener, w);

	w->surface = wl_compositor_create_surface(w->compositor);
	w->xdg_surface = wl_proxy_marshal_flags(w->wm_base, 2, &fg_xdg_surface_interface, wl_proxy_get_version(w->wm_base), 0, NULL, w->surface);
	wl_proxy_add_listener(w->xdg_surface, (void (**)(void)) & fg_xdg_surface_listener, w);
	w->toplevel = wl_proxy_marshal_flags(w->xdg_surface, 1, &fg_xdg_toplevel_interface, wl_proxy_get_version(w->xdg_surface), 0, NULL);
	wl_proxy_add_listener(w->toplevel, (void (**)(void)) & fg_toplevel_listener, w);
	wl_proxy_marshal_flags(w->toplevel, 2, NULL, wl_proxy_get_version(w->toplevel), 0, title);
	wl_proxy_marshal_flags(w->toplevel, 3, NULL, wl_proxy_get_version(w->toplevel), 0, app_id);
	wl_surface_commit(w->surface); // initial commit without a buffer: asks for a configure
	if (wl_display_roundtrip(w->display) < 0) { *err = "roundtrip failed"; return NULL; }
	return w;
}

// fg_next dispatches until there is something for Go: FG_READY (first frame committed),
// FG_KEY (*out filled), FG_CLOSED or FG_ERROR.
static int fg_next(fg_wl *w, fg_key *out) {
	for (;;) {
		if (w->ready_pending) {
			w->ready_pending = 0;
			return FG_READY;
		}
		if (w->head != w->tail) {
			*out = w->queue[w->head];
			w->head = (w->head + 1) % FG_QUEUE;
			return FG_KEY;
		}
		if (w->closed) return FG_CLOSED;
		if (wl_display_dispatch(w->display) < 0) return FG_ERROR;
	}
}

#endif
