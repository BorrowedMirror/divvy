package platformtest

import (
	"context"
	"sync"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// streamBuffer is how many undelivered events a fake stream holds; an emit into a full buffer
// reports false.
const streamBuffer = 1024

// stream is an event stream that can be subscribed to once: later calls get
// ErrAlreadyStarted. Events emitted before the subscription are buffered and delivered on
// subscribe. The owner's mutex (mu) guards it; the stream closes when the subscriber's context
// ends.
type stream[T any] struct {
	ch      chan T
	started bool
	closed  bool
	pending []T
}

func (s *stream[T]) subscribe(ctx context.Context, mu *sync.Mutex) (<-chan T, error) {
	if s.started {
		return nil, platform.ErrAlreadyStarted
	}
	s.started = true
	s.ch = make(chan T, max(streamBuffer, len(s.pending)))
	for _, ev := range s.pending {
		s.ch <- ev
	}
	s.pending = nil
	context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		s.close()
	})
	return s.ch, nil
}

// emit reports whether the event reached a live receiver (or was buffered before the
// subscription).
func (s *stream[T]) emit(ev T) bool {
	switch {
	case s.ch != nil && !s.closed:
		select {
		case s.ch <- ev:
			return true
		default:
			return false
		}
	case !s.started:
		s.pending = append(s.pending, ev)
		return true
	}
	return false
}

// close closes the channel, as when the subscriber goes away: later emits report false.
func (s *stream[T]) close() {
	if s.ch != nil && !s.closed {
		s.closed = true
		close(s.ch)
	}
}
