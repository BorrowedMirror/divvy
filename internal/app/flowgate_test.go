package app

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowGateSerialisesAndDebounces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := &flowGate{debounce: flowGateDebounce}
		t0 := time.Now()
		ms := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }
		p, ok := gate.tryBegin(t0)
		require.True(t, ok, "first press runs")
		_, ok = gate.tryBegin(ms(500))
		assert.False(t, ok, "dropped while in flight")
		p.release()
		_, ok = gate.tryBegin(ms(100))
		assert.False(t, ok, "debounced")
		p2, ok := gate.tryBegin(ms(200))
		require.True(t, ok, "after debounce")
		p.release() // a stale permit does not end the running flow
		_, ok = gate.tryBegin(ms(1000))
		assert.False(t, ok, "stale release let a second flow in")
		p2.release()
		_, ok = gate.tryBegin(t0)
		assert.False(t, ok, "an earlier instant is too soon")
		time.Sleep(time.Second)
		_, ok = gate.tryBegin(time.Now())
		assert.True(t, ok, "a press a second later runs")
	})
}
