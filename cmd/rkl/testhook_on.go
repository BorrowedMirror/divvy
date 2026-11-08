//go:build testhook

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/shell"
)

// domTimeout is how long the `dom` hook command waits for a web view's reply.
const domTimeout = 5 * time.Second

// startTestHook serves the e2e hook on the port in RKL_TEST_HOOK (0 picks one) until ctx is
// done (test-hook builds only). When RKL_TEST_HOOK_PORT_FILE names a file, the bound port is
// written to it, so a harness can start the app on port 0 without racing for a free port.
func startTestHook(ctx context.Context, s *shell.Shell, flow *app.Flow) {
	port, err := strconv.Atoi(os.Getenv("RKL_TEST_HOOK"))
	if err != nil || port < 0 || port > 65535 {
		return
	}
	st := flow.State()
	dom := app.NewDOMBridge(func(event string, payload any) { s.App.Event.Emit(event, payload) }, domTimeout)
	s.App.Event.On(app.DOMReplyEvent, func(e *application.CustomEvent) { dom.OnReply(e.Data) })
	bound, err := flow.StartTestHook(ctx, port, app.HookActions{
		TrayPresent: func() bool { return s.Tray != nil },
		Tray: func(item string) (bool, error) {
			if s.Tray == nil {
				return false, nil
			}
			switch item {
			case "open_settings":
				flow.ShowSettings("settings")
			case "open_diagnostics":
				flow.ShowSettings("diagnostics")
			case "price_check":
				flow.Trigger(true)
			case "quit":
				// After the reply has gone out: quitting closes the hook's connections.
				time.AfterFunc(50*time.Millisecond, s.App.Quit)
			}
			return true, nil
		},
		Quit: s.App.Quit,
		DOM:  dom.Query,
	})
	if err != nil {
		st.RecordError("test hook", err)
		return
	}
	if path := os.Getenv("RKL_TEST_HOOK_PORT_FILE"); path != "" {
		if err := writePortFile(path, bound); err != nil {
			st.RecordError("test hook port file", err)
		}
	}
}

// writePortFile writes port to path atomically, so a reader never sees a partial number.
func writePortFile(path string, port int) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, fmt.Appendf(nil, "%d\n", port), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
