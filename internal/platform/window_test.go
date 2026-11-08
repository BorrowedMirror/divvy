package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func gameAt(id uint64, x int32, focused bool) *WindowInfo {
	return &WindowInfo{Title: GameWindowTitle, Rect: Rect{X: x, Width: 800, Height: 600}, Focused: focused, NativeID: id}
}

func TestDiffWindowsCoversEveryTransition(t *testing.T) {
	assert.Empty(t, DiffWindows(nil, nil))
	assert.Equal(t, []WindowEvent{{Type: WindowFound, Info: *gameAt(1, 0, true)}}, DiffWindows(nil, gameAt(1, 0, true)))
	assert.Equal(t, []WindowEvent{{Type: WindowLost}}, DiffWindows(gameAt(1, 0, true), nil))
	assert.Empty(t, DiffWindows(gameAt(1, 0, true), gameAt(1, 0, true)))
	assert.Equal(t, []WindowEvent{
		{Type: WindowMoved, Rect: Rect{X: 5, Width: 800, Height: 600}},
		{Type: WindowFocusChanged, Focused: false},
	}, DiffWindows(gameAt(1, 0, true), gameAt(1, 5, false)))
	assert.Equal(t, []WindowEvent{{Type: WindowFocusChanged, Focused: true}}, DiffWindows(gameAt(1, 0, false), gameAt(1, 0, true)))
}

// A restarted game (new native id) is one Found, never Lost then Found: the app moves whatever
// follows the game to the new window instead of closing it.
func TestDiffWindowsReportsANewWindowAsFoundOnly(t *testing.T) {
	assert.Equal(t, []WindowEvent{{Type: WindowFound, Info: *gameAt(2, 7, false)}}, DiffWindows(gameAt(1, 0, true), gameAt(2, 7, false)))
}

func TestTrySendNeverBlocks(t *testing.T) {
	ch := make(chan int, 1)
	assert.True(t, TrySend(ch, 1))
	assert.False(t, TrySend(ch, 2), "full channel drops")
	assert.Equal(t, 1, <-ch)
}

func TestPermissionSettingsURLs(t *testing.T) {
	for _, p := range []Permission{PermissionAccessibility, PermissionScreenRecording, PermissionInputMonitoring} {
		url, ok := p.SettingsURL()
		assert.True(t, ok, p)
		assert.Contains(t, url, "x-apple.systempreferences:com.apple.preference.security?Privacy_", p)
	}
	_, ok := Permission("camera").SettingsURL()
	assert.False(t, ok)
}
