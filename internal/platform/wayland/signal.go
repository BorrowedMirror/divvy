//go:build unix

package wayland

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// signalBuffer is how many triggers may wait unread; further ones are dropped (as Rust's
// bounded channel did), which also absorbs a burst of signals.
const signalBuffer = 16

// actionForSignal is the action a trigger signal stands for.
func actionForSignal(sig os.Signal) (platform.HotkeyAction, bool) {
	switch sig {
	case syscall.SIGUSR2:
		return platform.Action(platform.PriceCheck), true
	case syscall.SIGUSR1:
		return platform.Action(platform.PriceCheckPinned), true
	}
	return platform.HotkeyAction{}, false
}

// JSCSignalForGC is the signal [MoveJSCSignal] gives WebKit's JavaScriptCore: a real-time
// signal that neither Go, glibc (32-34) nor musl (32-34) reserve.
const JSCSignalForGC = "40"

// MoveJSCSignal points JavaScriptCore's garbage-collector signal away from SIGUSR1, unless the
// user set JSC_SIGNAL_FOR_GC. WebKitGTK's JSC, which runs in our (UI) process, takes over
// SIGUSR1 by default ("Overriding existing handler for signal 10") and then crashes the process
// (SIGSEGV during cgo execution) when `pkill -USR1 rkl` arrives. It must run before the first
// web view is created (in practice: at the top of main, with [SignalTriggers]); with cgo, Go's
// os.Setenv updates the C environment JSC reads. Needed on every Linux backend, not only here.
func MoveJSCSignal() {
	if os.Getenv("JSC_SIGNAL_FOR_GC") == "" {
		_ = os.Setenv("JSC_SIGNAL_FOR_GC", JSCSignalForGC)
	}
}

// SignalTriggers starts listening for SIGUSR2 (PriceCheck) and SIGUSR1 (PriceCheckPinned) and
// returns the stream of fired triggers. It is the hotkey fallback when there is no
// GlobalShortcuts portal: users bind `pkill -USR2 rkl` / `pkill -USR1 rkl` in their compositor.
//
// Call it once, first thing in main: until it runs, SIGUSR1 and SIGUSR2 terminate the process.
// Call [MoveJSCSignal] too, or WebKit takes SIGUSR1 over.
// stop unsubscribes and closes the stream; once no os/signal subscriber is left Go restores the
// default (terminating) action, so call it only at shutdown, if at all. Every subscriber in the
// process sees every signal, so the shell must use a single consumer.
func SignalTriggers() (events <-chan platform.HotkeyEvent, stop func()) {
	sigs := make(chan os.Signal, signalBuffer)
	signal.Notify(sigs, syscall.SIGUSR2, syscall.SIGUSR1)
	out := make(chan platform.HotkeyEvent, signalBuffer)
	done := make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case sig := <-sigs:
				action, ok := actionForSignal(sig)
				if !ok {
					continue
				}
				select {
				case out <- platform.HotkeyEvent{Action: action}:
				default: // full: drop, the user pressed faster than we price check
				}
			}
		}
	}()
	var once sync.Once
	return out, func() {
		once.Do(func() {
			signal.Stop(sigs)
			close(done)
		})
	}
}
