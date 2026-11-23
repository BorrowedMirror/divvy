//go:build linux

package wayland

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// portalTimeout is how long [GlobalHotkeys] waits for the portal.
const portalTimeout = 3 * time.Second

// probePortal checks that an XDG desktop portal implementing GlobalShortcuts answers on the
// session bus, by reading the interface's version property.
func probePortal(ctx context.Context) error {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return platform.Backend("GlobalShortcuts portal: %w", err)
	}
	defer conn.Close()
	var version dbus.Variant
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.portal.GlobalShortcuts", "version").Store(&version)
	if err != nil {
		return platform.Backend("GlobalShortcuts portal: %w", err)
	}
	return nil
}

// GlobalHotkeys is the GlobalHotkeys capability on Wayland-native: whether Wails' global
// shortcuts can work at all (they go through the XDG GlobalShortcuts portal), plus the matching
// notes (the signal fallback when there is no portal). Wails itself only logs a missing portal
// through the app's error handler, so the composition root asks here when it completes the
// capabilities. It waits at most a few seconds for the session bus.
func GlobalHotkeys(ctx context.Context) (ok bool, notes []string) {
	ctx, cancel := context.WithTimeout(ctx, portalTimeout)
	defer cancel()
	ok = probePortal(ctx) == nil
	return ok, hotkeyNotes(ok)
}
