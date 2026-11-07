//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// startSignalTriggers makes SIGUSR1 a pinned price check and SIGUSR2 a normal one, for
// compositor key bindings (`pkill -USR2 rkl`). SIGUSR1 is always ours (unhandled, it would end
// the process); SIGUSR2 is ours unless the Wayland backend already maps it.
func startSignalTriggers(flow *app.Flow) {
	sigs := []os.Signal{syscall.SIGUSR1}
	if app.ShellHandlesSIGUSR2(flow.State().Backend == platform.BackendWayland) {
		sigs = append(sigs, syscall.SIGUSR2)
	}
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, sigs...)
	go func() {
		for sig := range ch {
			flow.Trigger(sig == syscall.SIGUSR1)
		}
	}()
}
