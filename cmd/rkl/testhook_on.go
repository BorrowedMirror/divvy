//go:build testhook

package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/shell"
)

// startTestHook serves the e2e hook on RKL_TEST_HOOK (test-hook builds only).
func startTestHook(s *shell.Shell, flow *app.Flow) {
	port, ok := app.TestHookPort()
	if !ok {
		return
	}
	dom := newDOMBridge(s.App.Event, domTimeout)
	_, err := flow.StartTestHook(port, app.HookActions{
		TrayPresent: func() bool { return s.Tray != nil },
		Tray: func(item string) (bool, error) {
			if s.Tray == nil {
				return false, nil
			}
			switch item {
			case "open_settings":
				return true, flow.ShowSettings("settings")
			case "open_diagnostics":
				return true, flow.ShowSettings("diagnostics")
			case "price_check":
				flow.Trigger(true)
			case "quit":
				flow.State().Quitting.Store(true)
				s.App.Quit()
			}
			return true, nil
		},
		Quit: s.App.Quit,
		DOM:  dom.Query,
	})
	if err != nil {
		flow.State().RecordError(fmt.Sprintf("test hook: %v", err))
	}
}

// The events of the `dom` hook command, answered by web/src/lib/e2e.ts.
const (
	domQueryEvent = "e2e-query"
	domReplyEvent = "e2e-reply"
	domTimeout    = 5 * time.Second
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

// eventBus is the part of Wails' event manager the bridge uses.
type eventBus interface {
	On(name string, callback func(event *application.CustomEvent)) func()
	Emit(name string, data ...any) bool
}

// domBridge sends `e2e-query` events to the web views and matches their `e2e-reply` events
// to the waiting hook command by id.
type domBridge struct {
	events  eventBus
	timeout time.Duration
	next    atomic.Uint64

	mu      sync.Mutex
	pending map[string]chan domReply
}

func newDOMBridge(ev eventBus, timeout time.Duration) *domBridge {
	b := &domBridge{events: ev, timeout: timeout, pending: map[string]chan domReply{}}
	ev.On(domReplyEvent, b.onReply)
	return b
}

func (b *domBridge) onReply(e *application.CustomEvent) {
	raw, err := json.Marshal(e.Data)
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

// Query implements [app.HookActions.DOM].
func (b *domBridge) Query(window, selector string) (string, error) {
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
	b.events.Emit(domQueryEvent, domQuery{ID: id, Window: window, Selector: selector})
	select {
	case r := <-ch:
		r.ID = ""
		out, err := json.Marshal(r)
		return string(out), err
	case <-time.After(b.timeout):
		return "", fmt.Errorf("no reply from the %s window within %s (not loaded yet?)", window, b.timeout)
	}
}
