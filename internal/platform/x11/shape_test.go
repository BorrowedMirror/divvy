package x11

import (
	"slices"
	"testing"

	"github.com/jezek/xgb/xproto"
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
	if covered(s, 0, 0) || covered(s, 419, 0) || covered(s, 0, 599) || covered(s, 419, 599) {
		t.Fatal("corners must be cut")
	}
	if !covered(s, 12, 0) || !covered(s, 0, 12) || !covered(s, 210, 300) || !covered(s, 407, 599) {
		t.Fatal("inside must be covered")
	}
	if !covered(s, 0, 300) || !covered(s, 419, 300) {
		t.Fatal("straight edges are full")
	}
	// Every pixel row is covered exactly once.
	for y := range 600 {
		rows := 0
		for _, sp := range s {
			if y >= int(sp.Y) && y < int(sp.Y)+int(sp.Height) {
				rows++
			}
		}
		if rows != 1 {
			t.Fatalf("row %d covered %d times", y, rows)
		}
	}
}

func TestDegenerateSizes(t *testing.T) {
	if len(roundedRectSpans(0, 10, 4)) != 0 {
		t.Fatal("zero width")
	}
	if got := roundedRectSpans(10, 10, 0); !slices.Equal(got, []xproto.Rectangle{{X: 0, Y: 0, Width: 10, Height: 10}}) {
		t.Fatalf("radius 0: %v", got)
	}
	if tiny := roundedRectSpans(6, 6, 50); len(tiny) != 6 {
		t.Fatalf("radius clamps to half the size: %v", tiny)
	}
}
