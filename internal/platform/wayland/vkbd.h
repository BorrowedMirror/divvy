// zwp_virtual_keyboard_v1 client over a private Wayland connection, for vkbd_cgo.go.
//
// The protocol tables below are what `wayland-scanner private-code` / `client-header` generate
// for virtual-keyboard-unstable-v1.xml, made static so this header can live in a cgo preamble:
//
//   Copyright © 2008-2011  Kristian Høgsberg
//   Copyright © 2010-2013  Intel Corporation
//   Copyright © 2012-2013  Collabora, Ltd.
//   Copyright © 2018       Purism SPC
//
//   Permission is hereby granted, free of charge, to any person obtaining a copy of this
//   software and associated documentation files (the "Software"), to deal in the Software
//   without restriction, including without limitation the rights to use, copy, modify, merge,
//   publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons
//   to whom the Software is furnished to do so, subject to the following conditions: The above
//   copyright notice and this permission notice (including the next paragraph) shall be
//   included in all copies or substantial portions of the Software. THE SOFTWARE IS PROVIDED
//   "AS IS", WITHOUT WARRANTY OF ANY KIND.
#ifndef RKL_VKBD_H
#define RKL_VKBD_H

#define _GNU_SOURCE
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <time.h>
#include <unistd.h>
#include <wayland-client.h>
#include <xkbcommon/xkbcommon.h>

static const struct wl_interface rkl_vk_interface;

static const struct wl_interface *rkl_vk_types[] = {
	NULL, NULL, NULL, NULL, &wl_seat_interface, &rkl_vk_interface,
};

static const struct wl_message rkl_vk_requests[] = {
	{"keymap", "uhu", rkl_vk_types + 0},
	{"key", "uuu", rkl_vk_types + 0},
	{"modifiers", "uuuu", rkl_vk_types + 0},
	{"destroy", "", rkl_vk_types + 0},
};

static const struct wl_interface rkl_vk_interface = {
	"zwp_virtual_keyboard_v1", 1, 4, rkl_vk_requests, 0, NULL,
};

static const struct wl_message rkl_vk_manager_requests[] = {
	{"create_virtual_keyboard", "on", rkl_vk_types + 4},
};

static const struct wl_interface rkl_vk_manager_interface = {
	"zwp_virtual_keyboard_manager_v1", 1, 1, rkl_vk_manager_requests, 0, NULL,
};

enum {
	RKL_VK_OK = 0,
	RKL_VK_CONNECT = 1,
	RKL_VK_NO_SEAT = 2,
	RKL_VK_NO_MANAGER = 3,
	RKL_VK_KEYMAP = 4,
	RKL_VK_PROTOCOL = 5,
};

typedef struct {
	struct wl_display *display;
	struct wl_registry *registry;
	struct wl_seat *seat;
	struct wl_proxy *manager;
	struct wl_proxy *keyboard;
	struct xkb_context *xkb;
	struct xkb_keymap *keymap;
	struct xkb_state *state;
} rkl_vk;

static void rkl_vk_global(void *data, struct wl_registry *r, uint32_t name, const char *iface, uint32_t version) {
	rkl_vk *vk = data;
	(void)version;
	if (!vk->seat && strcmp(iface, wl_seat_interface.name) == 0) {
		vk->seat = wl_registry_bind(r, name, &wl_seat_interface, 1);
	} else if (!vk->manager && strcmp(iface, rkl_vk_manager_interface.name) == 0) {
		vk->manager = wl_registry_bind(r, name, &rkl_vk_manager_interface, 1);
	}
}

static void rkl_vk_global_remove(void *data, struct wl_registry *r, uint32_t name) {
	(void)data, (void)r, (void)name;
}

static const struct wl_registry_listener rkl_vk_registry_listener = {rkl_vk_global, rkl_vk_global_remove};

static void rkl_vk_close(rkl_vk *vk) {
	if (vk->keyboard) wl_proxy_marshal_flags(vk->keyboard, 3, NULL, 1, WL_MARSHAL_FLAG_DESTROY);
	if (vk->manager) wl_proxy_destroy(vk->manager);
	if (vk->seat) wl_seat_destroy(vk->seat);
	if (vk->registry) wl_registry_destroy(vk->registry);
	if (vk->display) wl_display_disconnect(vk->display);
	if (vk->state) xkb_state_unref(vk->state);
	if (vk->keymap) xkb_keymap_unref(vk->keymap);
	if (vk->xkb) xkb_context_unref(vk->xkb);
	memset(vk, 0, sizeof *vk);
}

// rkl_vk_open connects to display (NULL: $WAYLAND_DISPLAY), creates a virtual keyboard on the
// first seat and uploads an xkb keymap built from the XKB_DEFAULT_* environment (else "us").
// On failure everything is released and an RKL_VK_* code returned.
static int rkl_vk_open(rkl_vk *vk, const char *display) {
	memset(vk, 0, sizeof *vk);
	vk->display = wl_display_connect(display);
	if (!vk->display) return RKL_VK_CONNECT;
	vk->registry = wl_display_get_registry(vk->display);
	wl_registry_add_listener(vk->registry, &rkl_vk_registry_listener, vk);
	if (wl_display_roundtrip(vk->display) < 0) goto protocol;
	if (!vk->seat) { rkl_vk_close(vk); return RKL_VK_NO_SEAT; }
	if (!vk->manager) { rkl_vk_close(vk); return RKL_VK_NO_MANAGER; }

	vk->xkb = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
	if (vk->xkb) vk->keymap = xkb_keymap_new_from_names(vk->xkb, NULL, XKB_KEYMAP_COMPILE_NO_FLAGS);
	if (vk->keymap) vk->state = xkb_state_new(vk->keymap);
	char *text = vk->keymap ? xkb_keymap_get_as_string(vk->keymap, XKB_KEYMAP_FORMAT_TEXT_V1) : NULL;
	if (!vk->state || !text) { free(text); rkl_vk_close(vk); return RKL_VK_KEYMAP; }
	size_t size = strlen(text) + 1;
	int fd = memfd_create("rkl-keymap", MFD_CLOEXEC);
	int written = fd >= 0 && write(fd, text, size) == (ssize_t)size;
	free(text);
	if (!written) { if (fd >= 0) close(fd); rkl_vk_close(vk); return RKL_VK_KEYMAP; }

	vk->keyboard = wl_proxy_marshal_flags(vk->manager, 0, &rkl_vk_interface, 1, 0, vk->seat, NULL);
	// The fd is duplicated when the request is marshalled.
	wl_proxy_marshal_flags(vk->keyboard, 0, NULL, 1, 0, (uint32_t)WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1, fd, (uint32_t)size);
	close(fd);
	if (wl_display_roundtrip(vk->display) < 0) goto protocol;
	return RKL_VK_OK;

protocol:
	rkl_vk_close(vk);
	return RKL_VK_PROTOCOL;
}

static uint32_t rkl_vk_now_ms(void) {
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return (uint32_t)(ts.tv_sec * 1000 + ts.tv_nsec / 1000000);
}

// rkl_vk_key sends one evdev key event and, when it changes them, the resulting modifiers
// (a virtual keyboard's modifier state is the client's to report). 0 on success.
static int rkl_vk_key(rkl_vk *vk, uint32_t code, int down) {
	wl_proxy_marshal_flags(vk->keyboard, 1, NULL, 1, 0, rkl_vk_now_ms(), code, (uint32_t)(down ? 1 : 0));
	enum xkb_state_component changed = xkb_state_update_key(vk->state, code + 8, down ? XKB_KEY_DOWN : XKB_KEY_UP);
	if (changed) {
		wl_proxy_marshal_flags(vk->keyboard, 2, NULL, 1, 0,
			(uint32_t)xkb_state_serialize_mods(vk->state, XKB_STATE_MODS_DEPRESSED),
			(uint32_t)xkb_state_serialize_mods(vk->state, XKB_STATE_MODS_LATCHED),
			(uint32_t)xkb_state_serialize_mods(vk->state, XKB_STATE_MODS_LOCKED),
			(uint32_t)xkb_state_serialize_layout(vk->state, XKB_STATE_LAYOUT_EFFECTIVE));
	}
	return wl_display_flush(vk->display) < 0 ? -1 : 0;
}

// rkl_vk_sync waits until the compositor has processed everything sent. 0 on success.
static int rkl_vk_sync(rkl_vk *vk) {
	return wl_display_roundtrip(vk->display) < 0 ? -1 : 0;
}

#endif
