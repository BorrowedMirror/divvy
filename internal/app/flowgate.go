package app

import (
	"sync"
	"sync/atomic"
	"time"
)

// flowGateDebounce is the debounce of the app's [flowGate].
const flowGateDebounce = 150 * time.Millisecond

// flowGate serialises price-check flows: one copy at a time, and presses within the debounce
// window of the last accepted one are dropped (key repeat / storms). Overlapping copies could
// otherwise "restore" the clipboard to the item text captured by the other run. It is safe for
// concurrent use.
type flowGate struct {
	debounce time.Duration
	inFlight atomic.Bool
	mu       sync.Mutex
	last     time.Time // zero before the first flow
}

// flowPermit is held while a flow runs; [flowPermit.release] lets the next press through.
type flowPermit struct {
	gate     *flowGate
	released atomic.Bool
}

// release ends the flow. Only the first call has an effect.
func (p *flowPermit) release() {
	if p.released.CompareAndSwap(false, true) {
		p.gate.inFlight.Store(false)
	}
}

// tryBegin starts a flow at now, or returns false if one is running or the last start was too
// recent (a now before the last start counts as too recent).
func (g *flowGate) tryBegin(now time.Time) (*flowPermit, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.last.IsZero() && max(now.Sub(g.last), 0) < g.debounce {
		return nil, false
	}
	if !g.inFlight.CompareAndSwap(false, true) {
		return nil, false
	}
	g.last = now
	return &flowPermit{gate: g}, true
}
