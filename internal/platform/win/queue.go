package win

import "sync"

// queue is an unbounded hand-off from a Win32 message-loop thread to a Go channel, standing in
// for Rust's unbounded mpsc channel: push never blocks, so a stalled consumer can never stall
// the message loop (and with it the WinEvent hooks), and no window event is ever dropped.
type queue[T any] struct {
	mu      sync.Mutex
	pending []T
	wake    chan struct{}
	out     chan T
}

// newQueue starts the forwarding goroutine and returns the queue; read from q.out.
func newQueue[T any]() *queue[T] {
	q := &queue[T]{wake: make(chan struct{}, 1), out: make(chan T)}
	go q.run()
	return q
}

func (q *queue[T]) push(v T) {
	q.mu.Lock()
	q.pending = append(q.pending, v)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue[T]) run() {
	for range q.wake {
		for {
			q.mu.Lock()
			batch := q.pending
			q.pending = nil
			q.mu.Unlock()
			if len(batch) == 0 {
				break
			}
			for _, v := range batch {
				q.out <- v
			}
		}
	}
}
