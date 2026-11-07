package main

import (
	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/win"
)

func preInit(platform.BackendKind, *[]string) {}

// newParts builds the Windows backend. The low-level mouse hook is only installed when Ctrl +
// wheel stash tab switching is on: it adds latency to every mouse move.
func newParts(_ platform.BackendKind, wheel bool) (platform.Parts, error) {
	return win.New(win.WithMouseHook(wheel))
}

// permissionProbe: Windows needs no OS permissions.
func permissionProbe(platform.BackendKind) app.PermissionProbe { return nil }
