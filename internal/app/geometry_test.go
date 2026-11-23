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
	testCases := []struct {
		name     string
		card     xy[int32]
		game     platform.Rect
		scale    float64
		points   bool
		expected xy[float64]
	}{
		{
			"ScaleOne",
			pair[int32](3000, 300),
			game,
			1,
			false,
			pair(440.0, 300.0),
		},
		{
			"ScaleTwo",
			pair[int32](3000, 300),
			game,
			2,
			false,
			pair(220.0, 150.0),
		},
		{
			"CardLeftOfWindow",
			pair[int32](100, 50),
			game,
			1,
			false,
			pair(0.0, 50.0),
		},
		{
			"PointsWindow",
			pair[int32](400, 100),
			platform.Rect{X: 100, Y: 25, Width: 800, Height: 600},
			2,
			true,
			pair(100.0, 25.0),
		},
		{
			"UnknownScaleCountsAsOne",
			pair[int32](3000, 300),
			game,
			0,
			false,
			pair(440.0, 300.0),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, pair(cardInWindow(tc.card.x, tc.card.y, tc.game, tc.scale, tc.points)))
		})
	}
}

func TestPanelSideSettingPlacesTheCard(t *testing.T) {
	area := Area{X: 2560, Y: 0, Width: 3440, Height: 1440}
	testCases := []struct {
		name     string
		have     config.OverlaySide
		expected xy[int32]
	}{
		{
			"Cursor",
			config.SideCursor,
			pair[int32](3024, 400),
		},
		{
			"Left",
			config.SideLeft,
			pair[int32](2568, 400),
		},
		{
			"Right",
			config.SideRight,
			pair[int32](5572, 400),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, pair(panelPosition(tc.have, 3000, 700, 420, 600, area)))
		})
	}

	x, _ := panelPosition(config.SideRight, 3000, 700, 5000, 600, area)
	assert.Equal(t, int32(2560), x, "too wide for the right edge")
}

func TestOverlayFlipsNearRightEdgeAndClamps(t *testing.T) {
	screen := Area{X: 0, Y: 0, Width: 1920, Height: 1080}
	tiny := Area{X: 0, Y: 0, Width: 300, Height: 200}
	testCases := []struct {
		name     string
		cursor   xy[int32]
		screen   Area
		expected xy[int32]
	}{
		{
			"OpensRightOfCursor",
			pair[int32](100, 500),
			screen,
			pair[int32](124, 220),
		},
		{
			"FlipsNearRightEdge",
			pair[int32](1800, 500),
			screen,
			pair[int32](1356, 220),
		},
		{
			"ClampsToTopEdge",
			pair[int32](1800, 10),
			screen,
			pair[int32](1356, 0),
		},
		{
			"ClampsToBottomEdge",
			pair[int32](1800, 1070),
			screen,
			pair[int32](1356, 520),
		},
		{
			"PinsToOriginOnTinyScreen",
			pair[int32](10, 10),
			tiny,
			pair[int32](0, 0),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, pair(overlayPosition(tc.cursor.x, tc.cursor.y, 420, 560, tc.screen)))
		})
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
	testCases := []struct {
		name     string
		p        xy[int32]
		size     xy[int32]
		expected xy[int32]
	}{
		{
			"AlreadyInside",
			pair[int32](100, 100),
			pair[int32](420, 600),
			pair[int32](100, 100),
		},
		{
			"PastBottomRight",
			pair[int32](1800, 900),
			pair[int32](420, 600),
			pair[int32](1500, 480),
		},
		{
			"PastTopLeft",
			pair[int32](-50, -10),
			pair[int32](420, 600),
			pair[int32](0, 0),
		},
		{
			"TooWidePinsToLeftEdge",
			pair[int32](10, 10),
			pair[int32](3000, 600),
			pair[int32](0, 10),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, pair(clampInto(tc.p.x, tc.p.y, tc.size.x, tc.size.y, area)))
		})
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
	testCases := []struct {
		name     string
		wanted   float64
		monitor  float64
		expected float64
	}{
		{
			"RoundsUpContent",
			431.2,
			1440,
			432,
		},
		{
			"RaisesToMinimum",
			50,
			1440,
			minOverlayHeight,
		},
		{
			"CapsAtScreenMinusMargins",
			5000,
			1440,
			1440 - 2*overlayScreenMargin,
		},
		{
			"NaNContentUsesMinimum",
			math.NaN(),
			1440,
			minOverlayHeight,
		},
		{
			"InfiniteContentUsesMinimum",
			math.Inf(1),
			1440,
			minOverlayHeight,
		},
		{
			"SmallMonitorUsesMinimum",
			500,
			100,
			minOverlayHeight,
		},
		{
			"NaNMonitorUsesMinimum",
			500,
			math.NaN(),
			minOverlayHeight,
		},
		{
			"InfiniteMonitorKeepsContent",
			500,
			math.Inf(1),
			500,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.expected, fitOverlayHeight(tc.wanted, tc.monitor), 1e-9)
		})
	}
}

func TestKeepsWindowInsideTheMonitor(t *testing.T) {
	testCases := []struct {
		name     string
		pos      int32
		size     int32
		expected int32
	}{
		{
			"AlreadyInside",
			100,
			400,
			100,
		},
		{
			"PastFarEdge",
			1200,
			400,
			1040,
		},
		{
			"PastNearEdge",
			-20,
			400,
			0,
		},
		{
			"LargerThanMonitor",
			100,
			2000,
			0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, keepOnScreen(tc.pos, tc.size, 0, 1440))
		})
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
