//go:build unix

package platform

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalsMapToActions(t *testing.T) {
	a, ok := actionForSignal(syscall.SIGUSR2)
	assert.True(t, ok)
	assert.Equal(t, Action(PriceCheck), a)
	a, ok = actionForSignal(syscall.SIGUSR1)
	assert.True(t, ok)
	assert.Equal(t, Action(PriceCheckPinned), a)
	_, ok = actionForSignal(syscall.SIGTERM)
	assert.False(t, ok)
}

func recvTrigger(t *testing.T, ch <-chan HotkeyEvent) HotkeyEvent {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.True(t, ok, "stream closed")
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no trigger within 5s")
	}
	return HotkeyEvent{}
}

func raise(t *testing.T, sig syscall.Signal) {
	t.Helper()
	require.NoError(t, syscall.Kill(syscall.Getpid(), sig))
}

// One test raises both signals: every subscriber in the process sees each signal, so separate
// tests would observe each other's events. The test's own subscriber (alive for the whole test)
// keeps the default, terminating action away after the first one stops.
func TestSignalTriggers(t *testing.T) {
	keep := SignalTriggers(t.Context())

	t.Run("SIGUSR2 checks and SIGUSR1 pins", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		events := SignalTriggers(ctx)
		raise(t, syscall.SIGUSR2)
		assert.Equal(t, Action(PriceCheck), recvTrigger(t, events).Action)
		assert.Equal(t, Action(PriceCheck), recvTrigger(t, keep).Action)
		raise(t, syscall.SIGUSR1)
		assert.Equal(t, Action(PriceCheckPinned), recvTrigger(t, events).Action)
		assert.Equal(t, Action(PriceCheckPinned), recvTrigger(t, keep).Action)

		cancel()
		select {
		case _, ok := <-events:
			for ok { // drain, then closed
				_, ok = <-events
			}
		case <-time.After(5 * time.Second):
			t.Fatal("stream not closed after the context ended")
		}
	})

	t.Run("a full stream drops triggers", func(t *testing.T) {
		events := SignalTriggers(t.Context())
		// Wait for each signal to arrive (the OS merges a signal raised while one is pending).
		for range signalBuffer + 8 {
			raise(t, syscall.SIGUSR2)
			recvTrigger(t, keep)
		}
		assert.Eventually(t, func() bool { return len(events) == signalBuffer }, 5*time.Second, time.Millisecond)
	})
}

func TestMoveJSCSignalKeepsAUserChoice(t *testing.T) {
	t.Setenv("JSC_SIGNAL_FOR_GC", "")
	MoveJSCSignal()
	assert.Equal(t, JSCSignalForGC, os.Getenv("JSC_SIGNAL_FOR_GC"))
	t.Setenv("JSC_SIGNAL_FOR_GC", "45")
	MoveJSCSignal()
	assert.Equal(t, "45", os.Getenv("JSC_SIGNAL_FOR_GC"))
}
