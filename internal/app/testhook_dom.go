//go:build testhook

package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// The events of the `dom` hook command, answered by web/src/lib/e2e.ts.
const (
	// DOMQueryEvent asks the web views about an element.
	DOMQueryEvent = "e2e-query"
	// DOMReplyEvent is a web view's answer; pass its data to [DOMBridge.OnReply].
	DOMReplyEvent = "e2e-reply"
)

// domQuery asks the web view of Window about the first element matching Selector.
type domQuery struct {
	ID       string `json:"id"`
	Window   string `json:"window"`
	Selector string `json:"selector"`
}

// domReply is the web view's answer, and (without ID) the hook's reply line.
type domReply struct {
	ID      string `json:"id,omitempty"`
	Exists  bool   `json:"exists"`
	Visible bool   `json:"visible"`
	Text    string `json:"text"`
}

// DOMBridge sends [DOMQueryEvent] events to the web views and matches their [DOMReplyEvent]
// events to the waiting hook command by id. It implements [HookActions.DOM].
type DOMBridge struct {
	emit    func(event string, payload any)
	timeout time.Duration
	next    atomic.Uint64

	mu      sync.Mutex
	pending map[string]chan domReply
}

// NewDOMBridge returns a bridge that emits queries with emit and gives up on a reply after
// timeout.
func NewDOMBridge(emit func(event string, payload any), timeout time.Duration) *DOMBridge {
	return &DOMBridge{emit: emit, timeout: timeout, pending: map[string]chan domReply{}}
}

// OnReply takes the data of a [DOMReplyEvent].
func (b *DOMBridge) OnReply(data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	var r domReply
	if json.Unmarshal(raw, &r) != nil {
		return
	}
	b.mu.Lock()
	ch := b.pending[r.ID]
	delete(b.pending, r.ID)
	b.mu.Unlock()
	if ch != nil {
		ch <- r // buffered: never blocks
	}
}

// Query asks the web view of window about the first element matching selector and returns its
// reply line.
func (b *DOMBridge) Query(ctx context.Context, window, selector string) (string, error) {
	id := strconv.FormatUint(b.next.Add(1), 10)
	ch := make(chan domReply, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()
	b.emit(DOMQueryEvent, domQuery{ID: id, Window: window, Selector: selector})
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	select {
	case r := <-ch:
		r.ID = ""
		out, err := json.Marshal(r)
		return string(out), err
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("no reply from the %s window within %s (not loaded yet?)", window, b.timeout)
	}
}
