package x11

import (
	"os"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// GDKBackendFor is the GDK backend matching the platform backend: `wayland` for
// Wayland-native (layer-shell), `x11` for everything else, so window positions are global and
// the EWMH always-on-top hints work under XWayland too.
func GDKBackendFor(kind platform.BackendKind) string {
	if kind == platform.BackendWayland {
		return "wayland"
	}
	return "x11"
}

// PreInit pins GDK_BACKEND for kind. Call it at the top of main, after the backend has been
// detected and before application.New (which initialises GTK); GTK reads the variable once.
// It overrides the x11 default Wails' own package init sets only outside Wayland sessions.
func PreInit(kind platform.BackendKind) error {
	return os.Setenv("GDK_BACKEND", GDKBackendFor(kind))
}
