package main

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/wayland"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/x11"
)

// preInit runs before Wails initialises GTK and creates any web view: it points GTK at the X11
// backend for X11/XWayland (window positions are global there) and at Wayland for
// Wayland-native, and moves WebKitGTK's garbage-collector signal off SIGUSR1, which it would
// otherwise take over, crashing the app on the first `pkill -USR1 rkl`.
func preInit(kind platform.BackendKind, errs *[]string) {
	wayland.MoveJSCSignal()
	if err := x11.PreInit(kind); err != nil {
		*errs = append(*errs, fmt.Sprintf("GDK backend: %v", err))
	}
}

// newParts builds the X11 or Wayland-native backend. The X11 hook records the wheel only when
// Ctrl + wheel stash tab switching is on.
func newParts(kind platform.BackendKind, wheel bool) (platform.Parts, error) {
	if kind == platform.BackendWayland {
		return wayland.New(
			wayland.WithProbe(platform.EnvProbeFromEnv()),
			wayland.WithMainThread(func(f func()) { application.InvokeSync(f) }),
		)
	}
	return x11.New(x11.Options{HookMouse: wheel})
}

// permissionProbe: Linux needs no OS permissions; the report lists degradation notes only.
func permissionProbe(platform.BackendKind) app.PermissionProbe { return nil }
