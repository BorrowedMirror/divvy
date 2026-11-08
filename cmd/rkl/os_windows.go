package main

import (
	"context"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/win"
)

// preInit: nothing needs preparing before Wails starts here.
func preInit(platform.BackendKind) error { return nil }

// newParts builds the Windows backend. The low-level mouse hook is only installed when Ctrl +
// wheel stash tab switching is on: it adds latency to every mouse move.
func newParts(_ platform.BackendKind, wheel bool) (platform.Parts, error) {
	return win.New(win.Options{HookMouse: wheel})
}

// permissionProbe: Windows needs no OS permissions.
func permissionProbe(platform.BackendKind) app.PermissionProbe { return nil }

// globalHotkeys: Wails' global shortcuts always work here.
func globalHotkeys(context.Context, platform.BackendKind) (bool, []string) { return true, nil }
