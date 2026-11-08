package x11

import (
	"testing"

	"github.com/jezek/xgb/xproto"
	"github.com/stretchr/testify/assert"
)

func covered(spans []xproto.Rectangle, x, y int) bool {
	for _, s := range spans {
		if x >= int(s.X) && x < int(s.X)+int(s.Width) && y >= int(s.Y) && y < int(s.Y)+int(s.Height) {
			return true
		}
	}
	return false
}

func TestRoundedSpansCutTheCornersOnly(t *testing.T) {
	s := roundedRectSpans(420, 600, 12)
	for _, p := range [][2]int{{0, 0}, {419, 0}, {0, 599}, {419, 599}} {
		assert.False(t, covered(s, p[0], p[1]), "corner %v must be cut", p)
	}
	for _, p := range [][2]int{{12, 0}, {0, 12}, {210, 300}, {407, 599}, {0, 300}, {419, 300}} {
		assert.True(t, covered(s, p[0], p[1]), "%v must be covered", p)
	}
	// Every pixel row is covered exactly once.
	for y := range 600 {
		rows := 0
		for _, sp := range s {
			if y >= int(sp.Y) && y < int(sp.Y)+int(sp.Height) {
				rows++
			}
		}
		assert.Equal(t, 1, rows, "row %d", y)
	}
}

func TestDegenerateSizes(t *testing.T) {
	assert.Empty(t, roundedRectSpans(0, 10, 4), "zero width")
	assert.Equal(t, []xproto.Rectangle{{X: 0, Y: 0, Width: 10, Height: 10}}, roundedRectSpans(10, 10, 0), "radius 0")
	assert.Len(t, roundedRectSpans(6, 6, 50), 6, "radius clamps to half the size")
}
