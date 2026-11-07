//go:build unix

package main

import (
	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform/wayland"
)

var signalEvents <-chan platform.HotkeyEvent

// listenSignals takes SIGUSR1 and SIGUSR2 at the very top of main: unhandled, either ends the
// process, so a trigger during startup must not reach the default action.
func listenSignals() { signalEvents, _ = wayland.SignalTriggers() }

// startSignalTriggers makes SIGUSR2 a price check and SIGUSR1 a pinned one, for compositor key
// bindings (`pkill -USR2 rkl`) on every unix backend. One listener for both: a second would
// deliver each signal twice. Triggers that arrived during startup run now.
func startSignalTriggers(flow *app.Flow) {
	events := signalEvents
	go func() {
		for ev := range events {
			flow.Trigger(ev.Action.Kind == platform.PriceCheckPinned)
		}
	}()
}
