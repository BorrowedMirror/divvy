package main

import (
	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/mac"
)

func preInit(platform.BackendKind, *[]string) {}

// newParts builds the macOS backend; the event tap also listens to the wheel only when Ctrl +
// wheel stash tab switching is on.
func newParts(_ platform.BackendKind, wheel bool) (platform.Parts, error) {
	return mac.New(mac.OptionsFromEnv(wheel))
}

// permissionProbe checks Accessibility, Input Monitoring and Screen Recording live.
func permissionProbe(platform.BackendKind) app.PermissionProbe { return mac.TCCProbe{} }
