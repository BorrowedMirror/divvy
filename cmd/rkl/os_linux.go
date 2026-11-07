package main

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/wayland"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/x11"
)

// preInit points GTK at the X11 backend for X11/XWayland (window positions are global there)
// and at Wayland for Wayland-native, before Wails initialises GTK.
func preInit(kind platform.BackendKind, errs *[]string) {
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
