package x11

import (
	"math"

	"github.com/jezek/xgb/xproto"
)

// roundedRectSpans are row spans covering a width x height rectangle with corners rounded to
// radius (clamped to half the shorter side): one span per corner row plus the straight middle
// band.
func roundedRectSpans(width, height, radius uint32) []xproto.Rectangle {
	w, h := min(width, math.MaxUint16), min(height, math.MaxUint16)
	r := min(radius, w/2, h/2)
	if w == 0 || h == 0 {
		return nil
	}
	span := func(x, y, sw, sh uint32) xproto.Rectangle {
		return xproto.Rectangle{X: int16(x), Y: int16(y), Width: uint16(sw), Height: uint16(sh)}
	}
	out := make([]xproto.Rectangle, 0, 2*r+1)
	rf := float64(r)
	for row := range r {
		// Distance from the corner circle's centre to the middle of this row.
		dy := rf - float64(row) - 0.5
		inset := min(uint32(math.Round(rf-math.Sqrt(max(rf*rf-dy*dy, 0)))), w/2)
		out = append(out, span(inset, row, w-2*inset, 1), span(inset, h-1-row, w-2*inset, 1))
	}
	if h > 2*r {
		out = append(out, span(0, r, w, h-2*r))
	}
	return out
}
