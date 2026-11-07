//go:build testhook

package main

import (
	"fmt"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/shell"
)

// startTestHook serves the e2e hook on RKL_TEST_HOOK (test-hook builds only).
func startTestHook(s *shell.Shell, flow *app.Flow) {
	port, ok := app.TestHookPort()
	if !ok {
		return
	}
	_, err := flow.StartTestHook(port, app.HookActions{
		TrayPresent: func() bool { return s.Tray != nil },
		Tray: func(item string) (bool, error) {
			if s.Tray == nil {
				return false, nil
			}
			switch item {
			case "open_settings":
				return true, flow.ShowSettings("settings")
			case "open_diagnostics":
				return true, flow.ShowSettings("diagnostics")
			case "price_check":
				flow.Trigger(true)
			case "quit":
				flow.State().Quitting.Store(true)
				s.App.Quit()
			}
			return true, nil
		},
		Quit: s.App.Quit,
	})
	if err != nil {
		flow.State().RecordError(fmt.Sprintf("test hook: %v", err))
	}
}
