//go:build !testhook

package main

import (
	"context"

	"github.com/BorrowedMirror/divvy/internal/app"
	"github.com/BorrowedMirror/divvy/internal/shell"
)

// startTestHook: release builds have no test hook.
func startTestHook(context.Context, *shell.Shell, *app.Flow) {}
