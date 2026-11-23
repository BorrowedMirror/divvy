package app

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/platform"
)

type xy[T int32 | float64] struct{ x, y T }

func pair[T int32 | float64](x, y T) xy[T] { return xy[T]{x, y} }

func TestCardPositionInsideTheCoveringWindow(t *testing.T) {
	game := platform.Rect{X: 2560, Y: 0, Width: 3440, Height: 1440}
	for _, c := range []struct {
		card   xy[int32]
		game   platform.Rect
		scale  float64
		points bool
		want   xy[float64]
	}{
		{pair[int32](3000, 300), game, 1, false, pair(440.0, 300.0)},
		{pair[int32](3000, 300), game, 2, false, pair(220.0, 150.0)},
		{pair[int32](100, 50), game, 1, false, pair(0.0, 50.0)},
		{pair[int32](400, 100), platform.Rect{X: 100, Y: 25, Width: 800, Height: 600}, 2, true, pair(100.0, 25.0)},
		{pair[int32](3000, 300), game, 0, false, pair(440.0, 300.0)}, // an unknown scale counts as 1
	} {
		assert.Equal(t, c.want, pair(cardInWindow(c.card.x, c.card.y, c.game, c.scale, c.points)), "%+v", c)
	}
}

func TestPanelSideSettingPlacesTheCard(t *testing.T) {
	area := Area{X: 2560, Y: 0, Width: 3440, Height: 1440}
	for _, c := range []struct {
		side config.OverlaySide
		want xy[int32]
	}{
		{config.SideCursor, pair[int32](3024, 400)},
		{config.SideLeft, pair[int32](2568, 400)},
		{config.SideRight, pair[int32](5572, 400)},
	} {
		assert.Equal(t, c.want, pair(panelPosition(c.side, 3000, 700, 420, 600, area)), c.side)
	}
	x, _ := panelPosition(config.SideRight, 3000, 700, 5000, 600, area)
	assert.Equal(t, int32(2560), x, "too wide for the right edge")
}

func TestOverlayFlipsNearRightEdgeAndClamps(t *testing.T) {
	screen := Area{X: 0, Y: 0, Width: 1920, Height: 1080}
	tiny := Area{X: 0, Y: 0, Width: 300, Height: 200}
	for _, c := range []struct {
		cursor xy[int32]
		screen Area
		want   xy[int32]
	}{
		{pair[int32](100, 500), screen, pair[int32](124, 220)},
		{pair[int32](1800, 500), screen, pair[int32](1356, 220)},
		{pair[int32](1800, 10), screen, pair[int32](1356, 0)},
		{pair[int32](1800, 1070), screen, pair[int32](1356, 520)},
		{pair[int32](10, 10), tiny, pair[int32](0, 0)},
	} {
		assert.Equal(t, c.want, pair(overlayPosition(c.cursor.x, c.cursor.y, 420, 560, c.screen)), "%+v", c)
	}
}

func TestPointInRectIncludesEdges(t *testing.T) {
	in := func(px, py float64) bool { return pointInRect(px, py, 100, 50, 420, 600) }
	assert.True(t, in(100, 50))
	assert.True(t, in(520, 650))
	assert.True(t, in(300, 300))
	assert.False(t, in(99, 300))
	assert.False(t, in(300, 651))
}

func TestClampIntoKeepsPanelsOnScreen(t *testing.T) {
	area := Area{X: 0, Y: 0, Width: 1920, Height: 1080}
	for _, c := range []struct {
		p, size, want xy[int32]
	}{
		{pair[int32](100, 100), pair[int32](420, 600), pair[int32](100, 100)},
		{pair[int32](1800, 900), pair[int32](420, 600), pair[int32](1500, 480)},
		{pair[int32](-50, -10), pair[int32](420, 600), pair[int32](0, 0)},
		{pair[int32](10, 10), pair[int32](3000, 600), pair[int32](0, 10)}, // too wide: pin to the left edge
	} {
		assert.Equal(t, c.want, pair(clampInto(c.p.x, c.p.y, c.size.x, c.size.y, area)), "%+v", c)
	}
}

func TestIntersectAreas(t *testing.T) {
	game := Area{X: 2560, Y: 0, Width: 3440, Height: 1440}
	assert.Equal(t, game, intersect(game, game), "same areas")
	half := Area{X: 2560, Y: 0, Width: 1000, Height: 1440}
	assert.Equal(t, half, intersect(game, half))
	apart := Area{X: 0, Y: 0, Width: 10, Height: 10}
	assert.Equal(t, game, intersect(game, apart), "no overlap keeps the first area")
}

func TestOverlayHeightFitsContentWithinTheScreen(t *testing.T) {
	for _, c := range []struct{ wanted, monitor, want float64 }{
		{431.2, 1440, 432},
		{50, 1440, minOverlayHeight},
		{5000, 1440, 1440 - 2*overlayScreenMargin},
		{math.NaN(), 1440, minOverlayHeight},
		{math.Inf(1), 1440, minOverlayHeight},
		{500, 100, minOverlayHeight},
		{500, math.NaN(), minOverlayHeight},
		{500, math.Inf(1), 500},
	} {
		assert.InDelta(t, c.want, fitOverlayHeight(c.wanted, c.monitor), 1e-9, "%+v", c)
	}
}

func TestKeepsWindowInsideTheMonitor(t *testing.T) {
	for _, c := range []struct{ pos, size, want int32 }{{100, 400, 100}, {1200, 400, 1040}, {-20, 400, 0}, {100, 2000, 0}} {
		assert.Equal(t, c.want, keepOnScreen(c.pos, c.size, 0, 1440), "%+v", c)
	}
}

func TestPanelPositionRoundTripsAndToleratesBadFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", positionFile)
	_, ok := loadPosition(path)
	assert.False(t, ok)
	require.NoError(t, savePosition(path, point{2600, -40}))
	p, ok := loadPosition(path)
	assert.True(t, ok)
	assert.Equal(t, point{2600, -40}, p)
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o644))
	_, ok = loadPosition(path)
	assert.False(t, ok)
}
