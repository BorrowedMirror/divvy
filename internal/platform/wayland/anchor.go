package wayland

import "github.com/BorrowedMirror/divvy/internal/platform"

// Anchor is where a layer surface sits: against the right (or left) output edge, Top and Side
// pixels in from the top and that edge.
type Anchor struct {
	Right     bool
	Top, Side int32
}

// AnchorFor turns a panel rect inside area (the output) into a layer-shell anchor: layer
// surfaces ignore positions, so the panel is pinned to the nearer vertical edge instead.
func AnchorFor(rect, area platform.Rect) Anchor {
	x, w := rect.X, int32(rect.Width)
	right := x > area.X+int32(area.Width)/2
	side := x - area.X
	if right {
		side = area.X + int32(area.Width) - (x + w)
	}
	return Anchor{Right: right, Top: rect.Y - area.Y, Side: max(side, 0)}
}
