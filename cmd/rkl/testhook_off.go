//go:build !testhook

package main

import (
	"context"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/shell"
)

// startTestHook: release builds have no test hook.
func startTestHook(context.Context, *shell.Shell, *app.Flow) {}
