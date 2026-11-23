package x11

// Game window tracking on X11: _NET_ACTIVE_WINDOW/_NET_CLIENT_LIST PropertyNotify on the root,
// ConfigureNotify (SubstructureNotify on root catches WM frames moving; StructureNotify on the
// client catches synthetic ICCCM configures) and title changes on the game window.
//
// The event goroutine blocks on the event connection (no busy polling). It wakes for relevant
// events, or every rescanInterval for a full rescan (titles set after mapping, odd WMs), and
// exits when the connection breaks (after sending Lost) or the subscription's context ends.

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// rescanInterval is the rescan interval when no relevant event arrived.
const rescanInterval = time.Second

// tracker is the X11 [platform.WindowTracker].
type tracker struct {
	conn       *xConn // for GameWindow and IsGameFocused
	display    string
	subscribed atomic.Bool
}

var _ platform.WindowTracker = (*tracker)(nil)

func newTracker(conn *xConn, display string) *tracker {
	return &tracker{conn: conn, display: display}
}

// GameWindow implements [platform.WindowTracker].
func (t *tracker) GameWindow() (platform.WindowInfo, bool) {
	w, ok := t.conn.findGame()
	if !ok {
		return platform.WindowInfo{}, false
	}
	return t.conn.info(w)
}

// IsGameFocused implements [platform.WindowTracker].
func (t *tracker) IsGameFocused() bool {
	w, ok := t.conn.findGame()
	return ok && t.conn.isFocused(w)
}

// Subscribe implements [platform.WindowTracker].
func (t *tracker) Subscribe(ctx context.Context) (<-chan platform.WindowEvent, error) {
	if !t.subscribed.CompareAndSwap(false, true) {
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
	go trackEvents(ctx, conn, events, out)
	return out, nil
}

// trackEvents is the event goroutine: it rescans when a relevant event arrived or rescanInterval
// passed, and sends the differences.
func trackEvents(ctx context.Context, conn *xConn, events <-chan xgb.Event, out chan<- platform.WindowEvent) {
	defer close(out)
	defer conn.close()
	send := func(ev platform.WindowEvent) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	w := watched{
		root:       conn.root,
		rootAtoms:  [2]xproto.Atom{conn.atoms.netActiveWindow, conn.atoms.netClientList},
		titleAtoms: [2]xproto.Atom{conn.atoms.netWMName, xproto.AtomWmName},
	}
	var state *platform.WindowInfo
	dirty := true
	lastScan := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
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
		if ctx.Err() != nil {
			return
		}
		if dirty || time.Since(lastScan) >= rescanInterval {
			dirty = false
			lastScan = time.Now()
			cur := snapshot(conn)
			if cur != nil && (state == nil || state.NativeID != cur.NativeID) {
				// Watch the client itself for synthetic ConfigureNotify, unmap/destroy, title.
				w.game = xproto.Window(cur.NativeID)
				xproto.ChangeWindowAttributes(conn.c, w.game, xproto.CwEventMask,
					[]uint32{xproto.EventMaskStructureNotify | xproto.EventMaskPropertyChange})
			}
			for _, ev := range platform.DiffWindows(state, cur) {
				if !send(ev) {
					return
				}
			}
			state = cur
			if state == nil {
				w.game = 0
			}
		}
		timer.Reset(max(rescanInterval-time.Since(lastScan), time.Millisecond))
		select {
		case ev, ok := <-events:
			if !ok {
				send(platform.WindowEvent{Type: platform.WindowLost})
				return
			}
			dirty = dirty || w.relevant(ev)
		case <-timer.C:
		case <-ctx.Done():
			return
		}
	}
}

// snapshot is the game window right now, or nil.
func snapshot(conn *xConn) *platform.WindowInfo {
	w, ok := conn.findGame()
	if !ok {
		return nil
	}
	info, ok := conn.info(w)
	if !ok {
		return nil
	}
	return &info
}
