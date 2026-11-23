package app

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/ipc"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/platform/platformtest"
)

func found(r platform.Rect) platform.WindowEvent {
	return platform.WindowEvent{Type: platform.WindowFound, Info: platformtest.GameWindow(r)}
}

func moved(r platform.Rect) platform.WindowEvent {
	return platform.WindowEvent{Type: platform.WindowMoved, Rect: r}
}

// wait advances the bubble's clock by d and lets the flow's goroutines run.
func wait(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestNoticeOriginCentresBelowTheMiddle(t *testing.T) {
	x, y := noticeOrigin(platform.Rect{X: 100, Y: 50, Width: 1920, Height: 1080}, 300, 64)
	assert.InDelta(t, 100+(1920-300)/2.0, x, 1e-9)
	assert.InDelta(t, 50+720-32.0, y, 1e-9)
}

func TestReadyNoticeShowsOverANewGameWindowThenHides(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.onWindowEvent(found(game().Rect))

		require.True(t, d.notice.IsVisible(), "shown as soon as the game window is found")
		x, y, w, h := d.notice.Bounds()
		assert.Equal(t, [4]int32{810, 688, 300, 64}, [4]int32{x, y, w, h})
		assert.False(t, d.notice.IsFocused(), "the notice never takes focus")
		assert.Zero(t, d.raises(), "raised without focus")
		require.Len(t, d.named(ipc.EventReadyNotice), 1)
		assert.Equal(t, ipc.ReadyNotice{DurationMS: 4000}, d.named(ipc.EventReadyNotice)[0])

		wait(noticeDuration - time.Millisecond)
		assert.True(t, d.notice.IsVisible())
		wait(time.Millisecond)
		assert.False(t, d.notice.IsVisible())
	})
}

func TestReadyNoticeFollowsTheGameWindow(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.onWindowEvent(found(platform.Rect{X: 0, Y: 0, Width: 800, Height: 600}))
		a.onWindowEvent(moved(game().Rect))
		x, y, _, _ := d.notice.Bounds()
		assert.Equal(t, [2]int32{810, 688}, [2]int32{x, y})
		wait(noticeDuration)
		assert.False(t, d.notice.IsVisible(), "moving does not extend the notice")
	})
}

func TestReadyNoticeTimerLeavesANewerNoticeUp(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.onWindowEvent(found(game().Rect))
		wait(noticeDuration / 2)
		a.onWindowEvent(found(game().Rect))
		wait(noticeDuration / 2)
		assert.True(t, d.notice.IsVisible(), "the first notice's timer must not hide the second")
		wait(noticeDuration / 2)
		assert.False(t, d.notice.IsVisible())
	})
}

func TestReadyNoticeHidesWhenTheGameCloses(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.onWindowEvent(found(game().Rect))
		require.True(t, d.notice.IsVisible())
		a.onWindowEvent(platform.WindowEvent{Type: platform.WindowLost})
		assert.False(t, d.notice.IsVisible())
	})
}

func TestReadyNoticeGivesWayToThePricePanelAndSettings(t *testing.T) {
	st := newTestState(t)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.onWindowEvent(found(game().Rect))
		a.showOverlay()
		assert.False(t, d.notice.IsVisible(), "the notice must not cover the price panel")

		a.onWindowEvent(found(game().Rect))
		a.ShowSettings("settings")
		assert.False(t, d.notice.IsVisible(), "nor the settings")
	})
}

func TestReadyNoticeCanBeTurnedOff(t *testing.T) {
	st := newTestState(t)
	settings := st.Settings()
	settings.Overlay.ReadyNotice = false
	st.setSettings(settings)
	synctest.Test(t, func(t *testing.T) {
		a, d, _ := newTestAppOn(t, st, platformtest.Options{Window: game()})
		a.onWindowEvent(found(game().Rect))
		assert.False(t, d.notice.IsVisible())
		assert.Empty(t, d.named(ipc.EventReadyNotice))
	})
}
