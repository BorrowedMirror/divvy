//go:build linux

package x11

// Game window tracking on X11: _NET_ACTIVE_WINDOW/_NET_CLIENT_LIST PropertyNotify on the root,
// ConfigureNotify (SubstructureNotify on root catches WM frames moving; StructureNotify on the
// client catches synthetic ICCCM configures) and title changes on the game window.
//
// The event goroutine blocks on the event connection (no busy polling). It wakes for relevant
// events, or every rescanInterval for a full rescan (titles set after mapping, odd WMs), and
// exits when the connection breaks (after sending Lost) or the tracker is closed.

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// rescanInterval is the rescan interval when no relevant event arrived.
const rescanInterval = time.Second

// Tracker is the X11 [platform.WindowTracker].
type Tracker struct {
	conn       *xConn
	display    string
	subscribed atomic.Bool
	stop       chan struct{}
	stopOnce   sync.Once
}

func newTracker(conn *xConn, display string) *Tracker {
	return &Tracker{conn: conn, display: display, stop: make(chan struct{})}
}

// GameWindow implements [platform.WindowTracker].
func (t *Tracker) GameWindow() (platform.WindowInfo, bool) {
	w, ok := t.conn.findGame()
	if !ok {
		return platform.WindowInfo{}, false
	}
	return t.conn.info(w)
}

// IsGameFocused implements [platform.WindowTracker].
func (t *Tracker) IsGameFocused() bool {
	w, ok := t.conn.findGame()
	return ok && t.conn.isFocused(w)
}

// Close stops the event goroutine (its channel is closed) and the tracker's connections.
func (t *Tracker) Close() {
	t.stopOnce.Do(func() { close(t.stop) })
	t.conn.close()
}

// Subscribe implements [platform.WindowTracker].
func (t *Tracker) Subscribe() (<-chan platform.WindowEvent, error) {
	if t.subscribed.Swap(true) {
		return nil, platform.ErrAlreadyStarted
	}
	// Own connection: the event loop must not wait behind GameWindow callers' round trips.
	events := make(chan xgb.Event, 256)
	conn, err := connect(t.display, events)
	if err != nil {
		t.subscribed.Store(false)
		return nil, err
	}
	mask := []uint32{xproto.EventMaskPropertyChange | xproto.EventMaskSubstructureNotify}
	if err := xproto.ChangeWindowAttributesChecked(conn.c, conn.root, xproto.CwEventMask, mask).Check(); err != nil {
		conn.close()
		t.subscribed.Store(false)
		return nil, xErr(err)
	}
	out := make(chan platform.WindowEvent, 64)
	go t.run(conn, events, out)
	return out, nil
}

func (t *Tracker) run(conn *xConn, events <-chan xgb.Event, out chan<- platform.WindowEvent) {
	defer close(out)
	defer conn.close()
	send := func(ev platform.WindowEvent) bool {
		select {
		case out <- ev:
			return true
		case <-t.stop:
			return false
		}
	}
	var state *snapshotState
	dirty := true
	lastScan := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		w := watched{
			root:       conn.root,
			rootAtoms:  [2]xproto.Atom{conn.atoms.netActiveWindow, conn.atoms.netClientList},
			titleAtoms: [2]xproto.Atom{conn.atoms.netWMName, xproto.AtomWmName},
		}
		if state != nil {
			w.game = state.window
		}
		// Drain what is queued.
	drain:
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					send(platform.WindowEvent{Type: platform.WindowLost})
					return
				}
				dirty = dirty || w.relevant(ev)
			default:
				break drain
			}
		}
		select {
		case <-t.stop:
			return
		default:
		}
		if dirty || time.Since(lastScan) >= rescanInterval {
			dirty = false
			lastScan = time.Now()
			cur := snapshot(conn)
			if cur != nil && (state == nil || state.window != cur.window) {
				// Watch the client itself for synthetic ConfigureNotify, unmap/destroy, title.
				xproto.ChangeWindowAttributes(conn.c, cur.window, xproto.CwEventMask,
					[]uint32{xproto.EventMaskStructureNotify | xproto.EventMaskPropertyChange})
			}
			for _, ev := range diff(state, cur) {
				if !send(ev) {
					return
				}
			}
			state = cur
		}
		left := max(rescanInterval-time.Since(lastScan), time.Millisecond)
		timer.Reset(left)
		select {
		case ev, ok := <-events:
			if !ok {
				send(platform.WindowEvent{Type: platform.WindowLost})
				return
			}
			dirty = dirty || w.relevant(ev)
		case <-timer.C:
		case <-t.stop:
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

func snapshot(conn *xConn) *snapshotState {
	w, ok := conn.findGame()
	if !ok {
		return nil
	}
	info, ok := conn.info(w)
	if !ok {
		return nil
	}
	return &snapshotState{tracked: tracked{window: w, rect: info.Rect, focused: info.Focused}, info: info}
}
