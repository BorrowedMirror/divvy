package win

import "github.com/BorrowedMirror/rust-king-ledger/internal/platform"

// New builds the Windows backend parts. Nothing here can fail at construction: the hooks are
// installed by Subscribe and Start.
func New(opts Options) (platform.Parts, error) {
	return platform.Parts{
		Kind:           platform.BackendWindows,
		Capabilities:   capabilities(),
		WindowTracker:  windowTracker{},
		InputListener:  inputListener{hookMouse: opts.HookMouse},
		InputSimulator: sendInputSimulator{},
		Overlay:        newOverlay,
		Cursor:         cursor,
		// Nil: Windows gives focus back to the previous window when the panel hides, and a
		// forced SetForegroundWindow can be refused by the foreground lock and only flash the
		// game's taskbar button.
		ActivateWindow: nil,
	}, nil
}

// cursor is the mouse position in physical screen pixels. GetPhysicalCursorPos is not
// DPI-virtualized, so it is right even if the process is not per-monitor DPI aware (for an
// aware process it equals GetCursorPos, the fallback).
func cursor() (x, y float64, ok bool) {
	var pt point
	if getPhysicalCursorPos(&pt) != nil && getCursorPos(&pt) != nil {
		return 0, 0, false
	}
	return float64(pt.x), float64(pt.y), true
}
