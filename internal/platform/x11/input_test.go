//go:build linux

package x11

import (
	"errors"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// noDisplay is a display name no X server can have.
const noDisplay = "rkl-disabled-display"

type countingSim struct{ calls int }

func (c *countingSim) SendChord(platform.Chord) error { c.calls++; return nil }

func TestLazyConnectionRetriesAndUsesFallback(t *testing.T) {
	sim := NewSimulator(noDisplay)
	chord, _ := platform.ParseChord("ctrl+c")
	if sim.EnsureReady() == nil {
		t.Fatal("cannot connect")
	}
	if err := sim.SendChord(chord); !errors.Is(err, platform.ErrBackend) {
		t.Fatalf("retried, still failing: %v", err)
	}
	if sim.Ready() {
		t.Fatal("not ready")
	}
	if err := sim.SendChord(platform.NewChord("NoSuchKey")); !errors.Is(err, platform.ErrInvalidChord) {
		t.Fatalf("invalid chord: %v", err)
	}

	fallback := &countingSim{}
	sim = NewSimulator(noDisplay).WithFallback(fallback)
	for range 2 {
		if err := sim.SendChord(chord); err != nil {
			t.Fatal(err)
		}
	}
	if fallback.calls != 2 {
		t.Fatalf("fallback calls = %d", fallback.calls)
	}
}

func TestNewFailsWithoutXServer(t *testing.T) {
	if _, err := New(Options{Display: noDisplay}); err == nil {
		t.Fatal("no X server")
	}
}
