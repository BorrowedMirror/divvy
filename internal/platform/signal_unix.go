//go:build unix

package platform

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalBuffer is how many triggers may wait unread; further ones are dropped, which also
// absorbs a burst of signals.
const signalBuffer = 16

// actionForSignal is the action a trigger signal stands for.
func actionForSignal(sig os.Signal) (HotkeyAction, bool) {
	switch sig {
	case syscall.SIGUSR2:
		return Action(PriceCheck), true
	case syscall.SIGUSR1:
		return Action(PriceCheckPinned), true
	}
	return HotkeyAction{}, false
}

// JSCSignalForGC is the signal [MoveJSCSignal] gives WebKit's JavaScriptCore: a real-time
// signal that neither Go, glibc (32-34) nor musl (32-34) reserve.
const JSCSignalForGC = "40"

// MoveJSCSignal points JavaScriptCore's garbage-collector signal away from SIGUSR1, unless the
// user set JSC_SIGNAL_FOR_GC. WebKitGTK's JSC, which runs in our (UI) process, takes over
// SIGUSR1 by default ("Overriding existing handler for signal 10") and then crashes the process
// (SIGSEGV during cgo execution) when `pkill -USR1 rkl` arrives. It must run before the first
// web view is created (in practice: at the top of main, with [SignalTriggers]); with cgo, Go's
// os.Setenv updates the C environment JSC reads. Needed on every Linux backend.
func MoveJSCSignal() {
	if os.Getenv("JSC_SIGNAL_FOR_GC") == "" {
		_ = os.Setenv("JSC_SIGNAL_FOR_GC", JSCSignalForGC)
	}
}

// SignalTriggers listens for SIGUSR2 (PriceCheck) and SIGUSR1 (PriceCheckPinned) until ctx is
// done and returns the stream of fired triggers, closed then. They are the hotkey fallback where
// global shortcuts do not work (no GlobalShortcuts portal): users bind `pkill -USR2 rkl` /
// `pkill -USR1 rkl` in their compositor.
//
// Call it once, first thing in main: until it runs, SIGUSR1 and SIGUSR2 terminate the process,
// and once ctx is done and no other os/signal subscriber is left they do again. Call
// [MoveJSCSignal] too, or WebKit takes SIGUSR1 over. Every subscriber in the process sees every
// signal, so the app must use a single consumer.
func SignalTriggers(ctx context.Context) <-chan HotkeyEvent {
	sigs := make(chan os.Signal, signalBuffer)
	signal.Notify(sigs, syscall.SIGUSR2, syscall.SIGUSR1)
	out := make(chan HotkeyEvent, signalBuffer)
	go func() {
		defer close(out)
		defer signal.Stop(sigs)
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-sigs:
				// A full stream drops the trigger: the user pressed faster than we price check.
				if action, ok := actionForSignal(sig); ok {
					TrySend(out, HotkeyEvent{Action: action})
				}
			}
		}
	}()
	return out
}
