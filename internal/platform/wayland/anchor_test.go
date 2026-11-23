package wayland

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestAnchorForPicksTheNearerEdge(t *testing.T) {
	area := platform.Rect{X: 1920, Y: 0, Width: 1920, Height: 1080}
	// Right half: anchored right, side = gap to the right edge.
	assert.Equal(t, Anchor{Right: true, Top: 100, Side: 20},
		AnchorFor(platform.Rect{X: 1920 + 1920 - 420 - 20, Y: 100, Width: 420, Height: 500}, area))
	// Left half: anchored left, side = gap to the left edge.
	assert.Equal(t, Anchor{Right: false, Top: 40, Side: 30},
		AnchorFor(platform.Rect{X: 1950, Y: 40, Width: 420, Height: 500}, area))
	// Overhanging the edge clamps the margin to 0.
	assert.Equal(t, Anchor{Right: true, Top: 0, Side: 0},
		AnchorFor(platform.Rect{X: 3800, Y: 0, Width: 420, Height: 500}, area))
}
