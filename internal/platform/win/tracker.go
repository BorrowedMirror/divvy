package win

import (
	"strings"
	"unicode"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// titleMatches reports whether a window title is the game's. Exact match (after trimming) so
// that e.g. a browser tab called "Path of Exile 2 - Wiki" is never mistaken for the game.
func titleMatches(title string) bool {
	return strings.TrimFunc(title, func(r rune) bool { return r == 0 || unicode.IsSpace(r) }) == platform.GameWindowTitle
}

// rectFromLTRB converts a Win32 RECT (left, top, right, bottom) into a [platform.Rect].
// Inverted rects clamp to 0.
func rectFromLTRB(left, top, right, bottom int32) platform.Rect {
	span := func(lo, hi int32) uint32 { return uint32(max(int64(hi)-int64(lo), 0)) }
	return platform.Rect{X: left, Y: top, Width: span(left, right), Height: span(top, bottom)}
}

// trackerState diffs raw observations into window events for the tracker. The Win32 hook
// thread feeds observations in and forwards whatever events come out.
type trackerState struct {
	current *platform.WindowInfo
}

// gameID is the native id of the tracked game window, if any.
func (s *trackerState) gameID() (uint64, bool) {
	if s.current == nil {
		return 0, false
	}
	return s.current.NativeID, true
}

// onScan takes a full rescan result (periodic timer, or after a foreground change while
// untracked); found is nil when there is no game window.
func (s *trackerState) onScan(found *platform.WindowInfo) []platform.WindowEvent {
	events := platform.DiffWindows(s.current, found)
	s.current = nil
	if found != nil {
		cur := *found
		s.current = &cur
	}
	return events
}

// onMoved handles the tracked window moving or resizing.
func (s *trackerState) onMoved(nativeID uint64, rect platform.Rect) (platform.WindowEvent, bool) {
	if s.current == nil || s.current.NativeID != nativeID || s.current.Rect == rect {
		return platform.WindowEvent{}, false
	}
	s.current.Rect = rect
	return platform.WindowEvent{Type: platform.WindowMoved, Rect: rect}, true
}

// onForeground handles the foreground window changing to foregroundID.
func (s *trackerState) onForeground(foregroundID uint64) (platform.WindowEvent, bool) {
	if s.current == nil {
		return platform.WindowEvent{}, false
	}
	focused := s.current.NativeID == foregroundID
	if s.current.Focused == focused {
		return platform.WindowEvent{}, false
	}
	s.current.Focused = focused
	return platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: focused}, true
}
