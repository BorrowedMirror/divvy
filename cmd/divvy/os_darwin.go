package main

import (
	"context"

	"github.com/BorrowedMirror/divvy/internal/app"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/platform/mac"
)

// preInit: nothing needs preparing before Wails starts here.
func preInit(platform.BackendKind) error { return nil }

// newParts builds the macOS backend; the event tap also listens to the wheel only when Ctrl +
// wheel stash tab switching is on.
func newParts(_ platform.BackendKind, wheel bool) (platform.Parts, error) {
	return mac.New(mac.OptionsFromEnv(wheel))
}

// permissionProbe checks Accessibility, Input Monitoring and Screen Recording live.
func permissionProbe(platform.BackendKind) app.PermissionProbe { return mac.TCCProbe{} }

// globalHotkeys: Wails' global shortcuts always work here.
func globalHotkeys(context.Context, platform.BackendKind) (bool, []string) { return true, nil }
