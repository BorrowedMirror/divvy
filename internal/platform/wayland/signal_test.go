//go:build unix

package wayland

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestSignalsMapToActions(t *testing.T) {
	a, ok := actionForSignal(syscall.SIGUSR2)
	assert.True(t, ok)
	assert.Equal(t, platform.Action(platform.PriceCheck), a)
	a, ok = actionForSignal(syscall.SIGUSR1)
	assert.True(t, ok)
	assert.Equal(t, platform.Action(platform.PriceCheckPinned), a)
	_, ok = actionForSignal(syscall.SIGTERM)
	assert.False(t, ok)
}

func recv(t *testing.T, ch <-chan platform.HotkeyEvent) platform.HotkeyEvent {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.True(t, ok, "stream closed")
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no trigger within 5s")
	}
	return platform.HotkeyEvent{}
}

// One test raises both signals: every subscriber in the process sees each signal, so separate
// tests would observe each other's events.
func TestSIGUSR2FiresPriceCheckAndSIGUSR1Pins(t *testing.T) {
	events, stop := SignalTriggers()
	// A second subscriber (kept for the whole test) keeps the default action, which would kill
	// the test binary, away after the first one stops.
	keep, stopKeep := SignalTriggers()
	defer stopKeep()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR2))
	assert.Equal(t, platform.Action(platform.PriceCheck), recv(t, events).Action)
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR1))
	assert.Equal(t, platform.Action(platform.PriceCheckPinned), recv(t, events).Action)
	assert.Equal(t, platform.Action(platform.PriceCheck), recv(t, keep).Action)

	stop()
	stop() // idempotent
	select {
	case _, ok := <-events:
		for ok { // drain, then closed
			_, ok = <-events
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream not closed after stop")
	}
}

func TestFullStreamDropsTriggers(t *testing.T) {
	events, stop := SignalTriggers()
	defer stop()
	for range signalBuffer + 8 {
		require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR2))
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	assert.LessOrEqual(t, len(events), signalBuffer)
	assert.Positive(t, len(events))
}

func TestMoveJSCSignalKeepsAUserChoice(t *testing.T) {
	t.Setenv("JSC_SIGNAL_FOR_GC", "")
	MoveJSCSignal()
	assert.Equal(t, JSCSignalForGC, os.Getenv("JSC_SIGNAL_FOR_GC"))
	t.Setenv("JSC_SIGNAL_FOR_GC", "45")
	MoveJSCSignal()
	assert.Equal(t, "45", os.Getenv("JSC_SIGNAL_FOR_GC"))
}
