package platform

import "os"

// GDKBackendFor is the GDK backend matching a platform backend: `wayland` for Wayland-native
// (layer-shell), `x11` for everything else, so window positions are global and the EWMH
// always-on-top hints work under XWayland too.
func GDKBackendFor(kind BackendKind) string {
	if kind == BackendWayland {
		return "wayland"
	}
	return "x11"
}

// SetGDKBackend pins GDK_BACKEND for kind. Call it at the top of main, after the backend has
// been detected and before application.New initialises GTK, which reads the variable once. It
// overrides the default Wails picks (x11 when XDG_SESSION_TYPE is unset or x11, and in its
// AppImage AppRun), so Wayland-native gets a Wayland GDK too.
func SetGDKBackend(kind BackendKind) error {
	return os.Setenv("GDK_BACKEND", GDKBackendFor(kind))
}
