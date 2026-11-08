package win

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func info(id uint64, rect platform.Rect, focused bool) platform.WindowInfo {
	return platform.WindowInfo{Title: platform.GameWindowTitle, Rect: rect, Focused: focused, NativeID: id}
}

var (
	r1 = platform.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}
	r2 = platform.Rect{X: 10, Y: 20, Width: 800, Height: 600}
)

func TestTitleMatchingIsExactAfterTrim(t *testing.T) {
	assert.True(t, titleMatches("Path of Exile 2"))
	assert.True(t, titleMatches("  Path of Exile 2\x00"))
	assert.False(t, titleMatches("Path of Exile"))
	assert.False(t, titleMatches("Path of Exile 2 - Wiki - Mozilla Firefox"))
	assert.False(t, titleMatches("path of exile 2"))
	assert.False(t, titleMatches(""))
}

func TestRectConversion(t *testing.T) {
	assert.Equal(t, r2, rectFromLTRB(10, 20, 810, 620))
	assert.Equal(t, platform.Rect{X: -8, Y: -8, Width: 1936, Height: 1096}, rectFromLTRB(-8, -8, 1928, 1088))
	assert.Equal(t, platform.Rect{X: 5, Y: 5, Width: 0, Height: 0}, rectFromLTRB(5, 5, 0, 0))
	assert.Equal(t, uint32(math.MaxUint32), rectFromLTRB(math.MinInt32, 0, math.MaxInt32, 1).Width)
}

func TestTrackerFoundMovedFocusLost(t *testing.T) {
	var s trackerState
	_, ok := s.gameID()
	assert.False(t, ok)
	assert.Empty(t, s.onScan(nil))
	_, ok = s.onForeground(1)
	assert.False(t, ok)
	_, ok = s.onMoved(1, r2)
	assert.False(t, ok)

	found := info(1, r1, false)
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowFound, Info: info(1, r1, false)}}, s.onScan(&found))
	id, ok := s.gameID()
	assert.True(t, ok)
	assert.Equal(t, uint64(1), id)
	same := info(1, r1, false)
	assert.Empty(t, s.onScan(&same))

	ev, ok := s.onForeground(1)
	assert.True(t, ok)
	assert.Equal(t, platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: true}, ev)
	_, ok = s.onForeground(1)
	assert.False(t, ok)
	ev, ok = s.onForeground(2)
	assert.True(t, ok)
	assert.Equal(t, platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: false}, ev)

	_, ok = s.onMoved(2, r2)
	assert.False(t, ok, "other window")
	ev, ok = s.onMoved(1, r2)
	assert.True(t, ok)
	assert.Equal(t, platform.WindowEvent{Type: platform.WindowMoved, Rect: r2}, ev)
	_, ok = s.onMoved(1, r2)
	assert.False(t, ok)

	back := info(1, r1, true)
	assert.Equal(t, []platform.WindowEvent{
		{Type: platform.WindowMoved, Rect: r1},
		{Type: platform.WindowFocusChanged, Focused: true},
	}, s.onScan(&back))

	// Game restarted: new HWND.
	restarted := info(3, r1, true)
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowFound, Info: info(3, r1, true)}}, s.onScan(&restarted))
	assert.Equal(t, []platform.WindowEvent{{Type: platform.WindowLost}}, s.onScan(nil))
	_, ok = s.gameID()
	assert.False(t, ok)
}

func TestTrackerKeepsItsOwnCopy(t *testing.T) {
	var s trackerState
	found := info(1, r1, false)
	s.onScan(&found)
	found.Rect = r2 // the caller's value must not alias the tracked state
	_, ok := s.onMoved(1, r1)
	assert.False(t, ok)
}
