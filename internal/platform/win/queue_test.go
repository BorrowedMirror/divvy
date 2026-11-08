package win

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
)

func TestQueueKeepsOrderAndNeverBlocks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := newQueue[int](t.Context())
		for i := range 1000 { // far more than any channel buffer: push must not block
			q.push(i)
		}
		q.close()
		var got []int
		for v := range q.out {
			got = append(got, v)
		}
		assert.Len(t, got, 1000)
		for i, v := range got {
			assert.Equal(t, i, v)
		}
	})
}

func TestQueueStopsWhenItsContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		q := newQueue[int](ctx)
		q.push(1)
		q.push(2)
		assert.Equal(t, 1, <-q.out)
		cancel() // nobody reads 2: the forwarding goroutine must not stay blocked on it
		synctest.Wait()
		_, ok := <-q.out
		assert.False(t, ok, "out closed, pending value dropped")
	})
}
