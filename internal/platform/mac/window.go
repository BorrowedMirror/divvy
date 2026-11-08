package mac

import (
	"math"
	"slices"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// rawWindow is one entry of CGWindowListCopyWindowInfo, reduced to plain values.
type rawWindow struct {
	// Number is kCGWindowNumber (CGWindowID).
	Number uint32
	// PID is kCGWindowOwnerPID.
	PID int32
	// Layer is kCGWindowLayer; 0 is the normal application window layer.
	Layer int64
	// OwnerName is kCGWindowOwnerName (process / app name, needs no permission); nil if absent.
	OwnerName *string
	// Title is kCGWindowName; nil when the process lacks Screen Recording permission (macOS
	// 10.15+ strips it for other apps' windows).
	Title *string
	// X, Y, Width, Height are kCGWindowBounds, in points, global top-left origin.
	X, Y, Width, Height float64
}

// minGameSide: windows smaller than this (points) are helper/tooltip windows, never the game.
const minGameSide = 64.0

// gameOwnerNames are the app/process names the game may run under. Used only when titles are
// hidden (no Screen Recording permission). The native client's bundle is named after the game.
var gameOwnerNames = []string{platform.GameWindowTitle, "PathOfExile", "PathOfExile2"}

// rectFromBounds converts kCGWindowBounds (floating point points) into a [platform.Rect],
// rounding and clamping to the field ranges (NaN becomes 0).
func rectFromBounds(x, y, width, height float64) platform.Rect {
	clampI32 := func(v float64) int32 {
		if math.IsNaN(v) {
			return 0
		}
		return int32(min(max(math.Round(v), math.MinInt32), math.MaxInt32))
	}
	clampU32 := func(v float64) uint32 {
		if math.IsNaN(v) {
			return 0
		}
		return uint32(min(max(math.Round(v), 0), math.MaxUint32))
	}
	return platform.Rect{X: clampI32(x), Y: clampI32(y), Width: clampU32(width), Height: clampU32(height)}
}

func isGameWindow(w *rawWindow) bool {
	if w.Layer != 0 || w.Width < minGameSide || w.Height < minGameSide {
		return false
	}
	if w.Title != nil {
		return *w.Title == platform.GameWindowTitle
	}
	// Title hidden: fall back to the owning process name.
	return w.OwnerName != nil && slices.Contains(gameOwnerNames, *w.OwnerName)
}

// findGameRaw finds the game window in an on-screen window list ordered front to back.
func findGameRaw(windows []rawWindow) *rawWindow {
	for i := range windows {
		if isGameWindow(&windows[i]) {
			return &windows[i]
		}
	}
	return nil
}

// isFrontmost reports whether pid owns the frontmost normal-layer window, i.e. is the active
// app. Overlays and menus live on higher layers and are ignored.
func isFrontmost(windows []rawWindow, pid int32) bool {
	for _, w := range windows {
		if w.Layer == 0 {
			return w.PID == pid
		}
	}
	return false
}

// findGame returns the game window as a [platform.WindowInfo], with Focused derived from the
// window order.
func findGame(windows []rawWindow) (platform.WindowInfo, bool) {
	w := findGameRaw(windows)
	if w == nil {
		return platform.WindowInfo{}, false
	}
	title := platform.GameWindowTitle
	if w.Title != nil {
		title = *w.Title
	}
	return platform.WindowInfo{
		Title:    title,
		Rect:     rectFromBounds(w.X, w.Y, w.Width, w.Height),
		Focused:  isFrontmost(windows, w.PID),
		NativeID: uint64(w.Number),
	}, true
}
