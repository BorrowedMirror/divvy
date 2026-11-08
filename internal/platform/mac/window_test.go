package mac

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func win(number uint32, pid int32, title, owner *string) rawWindow {
	return rawWindow{Number: number, PID: pid, OwnerName: owner, Title: title, X: 10.4, Y: 20.6, Width: 1280, Height: 720}
}

func TestRectRoundsAndClamps(t *testing.T) {
	assert.Equal(t, platform.Rect{X: 10, Y: -21, Width: 100, Height: 0}, rectFromBounds(10.4, -20.6, 99.5, 0.2))
	assert.Equal(t, platform.Rect{X: math.MaxInt32, Y: math.MinInt32, Width: 0, Height: 0},
		rectFromBounds(1e12, -1e12, -5.0, math.NaN()))
}

func TestFindsGameByTitleAndFocus(t *testing.T) {
	windows := []rawWindow{win(1, 10, new("Path of Exile 2"), new("Path of Exile 2")), win(2, 11, new("Finder"), nil)}
	info, ok := findGame(windows)
	require.True(t, ok)
	assert.Equal(t, uint64(1), info.NativeID)
	assert.Equal(t, "Path of Exile 2", info.Title)
	assert.Equal(t, platform.Rect{X: 10, Y: 21, Width: 1280, Height: 720}, info.Rect)
	assert.True(t, info.Focused)
}

func TestGameBehindAnotherWindowIsNotFocused(t *testing.T) {
	windows := []rawWindow{win(2, 11, new("Safari"), nil), win(1, 10, new("Path of Exile 2"), nil)}
	info, ok := findGame(windows)
	require.True(t, ok)
	assert.False(t, info.Focused)
	assert.False(t, isFrontmost(windows, 10))
	assert.True(t, isFrontmost(windows, 11))
}

func TestOverlayLayersDoNotStealFocus(t *testing.T) {
	overlay := win(3, 99, new("rkl"), nil)
	overlay.Layer = 25
	info, ok := findGame([]rawWindow{overlay, win(1, 10, new("Path of Exile 2"), nil)})
	require.True(t, ok)
	assert.True(t, info.Focused)
	assert.False(t, isFrontmost(nil, 10))
}

func TestFallsBackToOwnerNameWithoutTitles(t *testing.T) {
	info, ok := findGame([]rawWindow{win(5, 10, nil, new("Path of Exile 2"))})
	require.True(t, ok)
	assert.Equal(t, platform.GameWindowTitle, info.Title)
	assert.Equal(t, uint64(5), info.NativeID)
	_, ok = findGame([]rawWindow{win(6, 10, nil, new("Safari"))})
	assert.False(t, ok)
	_, ok = findGame([]rawWindow{win(6, 10, nil, nil)})
	assert.False(t, ok)
}

func TestIgnoresWrongTitleLayerAndTinyWindows(t *testing.T) {
	// A visible title that differs wins over a matching owner name.
	_, ok := findGame([]rawWindow{win(1, 10, new("Path of Exile 2 Launcher"), new("Path of Exile 2"))})
	assert.False(t, ok)
	menu := win(1, 10, new("Path of Exile 2"), nil)
	menu.Layer = 24
	_, ok = findGame([]rawWindow{menu})
	assert.False(t, ok)
	tiny := win(1, 10, new("Path of Exile 2"), nil)
	tiny.Width = 10
	_, ok = findGame([]rawWindow{tiny})
	assert.False(t, ok)
	short := win(1, 10, new("Path of Exile 2"), nil)
	short.Height = 10
	assert.Nil(t, findGameRaw([]rawWindow{short}))
}
