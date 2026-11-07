//go:build linux

package x11

// The passive input hook: the RECORD extension, as rdev did. The control connection (xgb)
// creates a context recording core device events from all clients; a raw data connection
// enables it and reads the reply stream on its own goroutine. Events are handed off with a
// non-blocking send into a bounded channel and dropped when the consumer lags.

import (
	"log/slog"
	"net"
	"sync"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/jezek/xgb/record"
)

// hookStarted is true while a hook runs (or starts): exactly one per process. A failed hook
// clears it so it can be started again.
var hookStarted struct {
	sync.Mutex
	on bool
}

// Listener is the X11 [platform.InputListener].
type Listener struct {
	display string
	mouse   bool

	mu   sync.Mutex
	stop func()
}

// NewListener records the keyboard; mouse also records pointer motion and buttons (Ctrl +
// wheel stash tab switching needs the wheel).
func NewListener(display string, mouse bool) *Listener {
	return &Listener{display: display, mouse: mouse}
}

// Start implements [platform.InputListener]. A second call while the hook lives returns
// [platform.ErrAlreadyStarted]. The hook is live when Start returns; if it dies later (the X
// server went away) the channel is closed and Start may be called again.
func (l *Listener) Start() (<-chan platform.InputEvent, error) {
	hookStarted.Lock()
	if hookStarted.on {
		hookStarted.Unlock()
		return nil, platform.ErrAlreadyStarted
	}
	hookStarted.on = true
	hookStarted.Unlock()
	ch, stop, err := startRecord(l.display, l.mouse)
	if err != nil {
		markHookStopped()
		return nil, err
	}
	l.mu.Lock()
	l.stop = stop
	l.mu.Unlock()
	return ch, nil
}

// Stop ends the hook (its channel is closed). The process-wide hook can then be started again.
func (l *Listener) Stop() {
	l.mu.Lock()
	stop := l.stop
	l.stop = nil
	l.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func markHookStopped() {
	hookStarted.Lock()
	hookStarted.on = false
	hookStarted.Unlock()
}

func startRecord(display string, mouse bool) (<-chan platform.InputEvent, func(), error) {
	ctrl, err := connect(display, nil)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (<-chan platform.InputEvent, func(), error) {
		ctrl.close()
		return nil, nil, err
	}
	if err := ctrl.useExtension("RECORD"); err != nil {
		return fail(err)
	}
	if _, err := record.QueryVersion(ctrl.c, 1, 13).Reply(); err != nil {
		return fail(xErr(err))
	}
	ctx, err := record.NewContextId(ctrl.c)
	if err != nil {
		return fail(xErr(err))
	}
	rng := record.Range{DeviceEvents: record.Range8{First: evKeyPress, Last: recordRangeLast(mouse)}}
	err = record.CreateContextChecked(ctrl.c, ctx, 0, 1, 1, []record.ClientSpec{record.CsAllClients}, []record.Range{rng}).Check()
	if err != nil {
		return fail(xErr(err))
	}
	ctrl.c.ExtLock.RLock()
	opcode := ctrl.c.Extensions["RECORD"]
	ctrl.c.ExtLock.RUnlock()

	data, err := dialRaw(display)
	if err != nil {
		return fail(xErr(err))
	}
	if _, err := data.Write(enableContextRequest(opcode, uint32(ctx))); err != nil {
		data.Close()
		return fail(xErr(err))
	}
	if cat, _, err := readRecordReply(data); err != nil || cat != recordStartOfData {
		data.Close()
		if err == nil {
			err = platform.Backend("x11: RECORD did not start (category %d)", cat)
		}
		return fail(xErr(err))
	}

	ch := make(chan platform.InputEvent, hookChannelCapacity)
	var once sync.Once
	stopping := make(chan struct{})
	finished := make(chan struct{})
	stop := func() {
		once.Do(func() {
			close(stopping)
			// DisableContext ends the stream with EndOfData; closing the socket covers a
			// server that is already gone.
			_ = record.DisableContextChecked(ctrl.c, ctx).Check()
			data.Close()
		})
		<-finished
	}
	go func() {
		defer close(finished)
		readRecord(data, ch, stopping)
		_ = record.FreeContextChecked(ctrl.c, ctx).Check()
		ctrl.close()
		// Free the process-wide hook before the consumer sees the channel close, so it can
		// start a new one right away.
		markHookStopped()
		close(ch)
	}()
	return ch, stop, nil
}

func readRecord(data net.Conn, ch chan<- platform.InputEvent, stopping <-chan struct{}) {
	defer data.Close()
	emit := func(ev platform.InputEvent) { forward(ch, ev) }
	for {
		cat, payload, err := readRecordReply(data)
		if err != nil {
			select {
			case <-stopping:
			default:
				slog.Warn("input hook stopped", "error", err)
			}
			return
		}
		switch cat {
		case recordFromServer:
			recordEvents(payload, emit)
		case recordEndOfData:
			return
		}
	}
}
