//go:build !unix

package platform

import "context"

// MoveJSCSignal does nothing: there is no WebKitGTK here.
func MoveJSCSignal() {}

// SignalTriggers returns a stream that never fires (there are no POSIX signals here; `divvy
// --trigger` works on every OS) and is closed when ctx is done.
func SignalTriggers(ctx context.Context) <-chan HotkeyEvent {
	out := make(chan HotkeyEvent)
	context.AfterFunc(ctx, func() { close(out) })
	return out
}
