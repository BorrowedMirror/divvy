package win

import (
	"context"
	"sync"
)

// queue is an unbounded hand-off from a Win32 message-loop thread to a Go channel: push never
// blocks, so a stalled consumer can never stall the message loop (and with it the WinEvent
// hooks), and no window event is dropped while someone listens.
type queue[T any] struct {
	ctx     context.Context
	mu      sync.Mutex
	pending []T
	closed  bool
	wake    chan struct{}
	out     chan T
}

// newQueue starts the forwarding goroutine and returns the queue; read from q.out. The
// goroutine ends, and closes out, after [queue.close] once everything pushed was delivered, or
// as soon as ctx is done (the consumer is gone; what is pending is dropped).
func newQueue[T any](ctx context.Context) *queue[T] {
	q := &queue[T]{ctx: ctx, wake: make(chan struct{}, 1), out: make(chan T)}
	go q.run()
	return q
}

// push queues v; it never blocks.
func (q *queue[T]) push(v T) {
	q.mu.Lock()
	q.pending = append(q.pending, v)
	q.mu.Unlock()
	q.signal()
}

// close ends the queue: out is closed after the values pushed so far.
func (q *queue[T]) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

func (q *queue[T]) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// take returns and clears the pending values, and whether the queue was closed.
func (q *queue[T]) take() ([]T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	batch := q.pending
	q.pending = nil
	return batch, q.closed
}

func (q *queue[T]) run() {
	defer close(q.out)
	for {
		select {
		case <-q.wake:
		case <-q.ctx.Done():
			return
		}
		for {
			batch, closed := q.take()
			if len(batch) == 0 {
				if closed {
					return
				}
				break
			}
			for _, v := range batch {
				select {
				case q.out <- v:
				case <-q.ctx.Done():
					return
				}
			}
		}
	}
}
