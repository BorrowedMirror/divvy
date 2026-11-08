package main

import (
	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// startSignalTriggers makes SIGUSR2 a price check and SIGUSR1 a pinned one, for compositor key
// bindings (`pkill -USR2 rkl`) on every unix backend. One listener for both: a second would
// deliver each signal twice. Triggers that arrived during startup run now. Without POSIX
// signals the stream never fires; `rkl --trigger` works on every OS.
func startSignalTriggers(flow *app.Flow, triggers <-chan platform.HotkeyEvent) {
	go func() {
		for ev := range triggers {
			flow.Trigger(ev.Action.Kind == platform.PriceCheckPinned)
		}
	}()
}
