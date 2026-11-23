package x11

// The passive input hook: the RECORD extension. The control connection (xgb) creates a context
// recording core device events from all clients; a raw data connection enables it and reads
// the reply stream on its own goroutine. Events are handed off with a non-blocking send into a
// bounded channel and dropped when the consumer lags.

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/jezek/xgb/record"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// hookStarted is true while a hook runs (or starts): exactly one per process.
var hookStarted atomic.Bool

// listener is the X11 [platform.InputListener].
type listener struct {
	display string
	mouse   bool
}

var _ platform.InputListener = (*listener)(nil)

// newListener records the keyboard; mouse also records pointer motion and buttons (Ctrl +
// wheel stash tab switching needs the wheel).
func newListener(display string, mouse bool) *listener {
	return &listener{display: display, mouse: mouse}
}

// Start implements [platform.InputListener]. The hook is live when Start returns. It ends,
// closing the channel, when ctx is done or the X server goes away; Start may then be called
// again.
func (l *listener) Start(ctx context.Context) (<-chan platform.InputEvent, error) {
	if !hookStarted.CompareAndSwap(false, true) {
		return nil, platform.ErrAlreadyStarted
	}
	ch, stop, err := startRecord(l.display, l.mouse)
	if err != nil {
		hookStarted.Store(false)
		return nil, err
	}
	context.AfterFunc(ctx, stop)
	return ch, nil
}

// startRecord enables a RECORD context on display and reads it on its own goroutine until the
// stream ends; stop ends it and waits for the reader.
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
	rc, err := record.NewContextId(ctrl.c)
	if err != nil {
		return fail(xErr(err))
	}
	rng := record.Range{DeviceEvents: record.Range8{First: evKeyPress, Last: recordRangeLast(mouse)}}
	err = record.CreateContextChecked(ctrl.c, rc, 0, 1, 1, []record.ClientSpec{record.CsAllClients}, []record.Range{rng}).Check()
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
	if _, err := data.Write(enableContextRequest(opcode, uint32(rc))); err != nil {
		data.Close()
		return fail(xErr(err))
	}
	if cat, _, err := readRecordReply(data); err != nil || cat != recordStartOfData {
		data.Close()
		if err == nil {
			return fail(platform.Backend("x11: RECORD did not start (category %d)", cat))
		}
		return fail(xErr(err))
	}

	ch := make(chan platform.InputEvent, platform.InputBufferSize)
	stopping := make(chan struct{})
	finished := make(chan struct{})
	stop := sync.OnceFunc(func() {
		close(stopping)
		// DisableContext ends the stream with EndOfData; closing the socket covers a server
		// that is already gone.
		_ = record.DisableContextChecked(ctrl.c, rc).Check()
		data.Close()
		<-finished
	})
	go func() {
		defer close(finished)
		readRecord(data, ch, stopping)
		_ = record.FreeContextChecked(ctrl.c, rc).Check()
		ctrl.close()
		// Free the process-wide hook before the consumer sees the channel close, so it can
		// start a new one right away.
		hookStarted.Store(false)
		close(ch)
	}()
	return ch, stop, nil
}

// readRecord forwards the recorded events until the stream ends or fails.
func readRecord(data net.Conn, ch chan<- platform.InputEvent, stopping <-chan struct{}) {
	defer data.Close()
	emit := func(ev platform.InputEvent) { platform.TrySend(ch, ev) }
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
