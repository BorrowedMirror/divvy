package app

import (
	"math"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Area is an integer rectangle in physical pixels.
type Area struct {
	X, Y, Width, Height int32
}

// OverlayPosition places a panel of size (w, h) beside the cursor (cx, cy), flipping to the
// left of the cursor when it would overflow the right edge of screen, and clamping vertically.
func OverlayPosition(cx, cy, w, h int32, screen Area) (x, y int32) {
	const gap = 24
	right := screen.X + screen.Width
	bottom := screen.Y + screen.Height
	if cx+gap+w <= right {
		x = cx + gap
	} else {
		x = max(cx-gap-w, screen.X)
	}
	y = min(max(cy-h/2, screen.Y), max(bottom-h, screen.Y))
	return x, y
}

// PanelPosition is the card position for the `overlay.side` setting: beside the cursor, or
// against the left/right edge of area (the game window when tracked, else the monitor),
// vertically centred on the cursor and clamped like the cursor placement.
func PanelPosition(side config.OverlaySide, cx, cy, w, h int32, area Area) (x, y int32) {
	const edge = 8
	x, y = OverlayPosition(cx, cy, w, h, area)
	switch side {
	case config.SideLeft:
		return area.X + edge, y
	case config.SideRight:
		return max(area.X+area.Width-w-edge, area.X), y
	default: // SideCursor, SideLast
		return x, y
	}
}

// MinOverlayHeight is the smallest overlay height we allow (logical px), so the header and
// empty state always fit.
const MinOverlayHeight = 120.0

// OverlayScreenMargin is the gap kept between the overlay and the top/bottom of the monitor
// (logical px).
const OverlayScreenMargin = 8.0

// FitOverlayHeight is the height (logical px) to give the overlay for content of wanted px on a
// monitor that is monitorHeight logical px tall: as tall as the content, never taller than the
// screen.
func FitOverlayHeight(wanted, monitorHeight float64) float64 {
	hi := rustMaxF64(monitorHeight-2*OverlayScreenMargin, MinOverlayHeight)
	if math.IsNaN(wanted) || math.IsInf(wanted, 0) {
		return MinOverlayHeight
	}
	return min(max(math.Ceil(wanted), MinOverlayHeight), hi)
}

// KeepOnScreenY is the top edge (physical px) that keeps a window of height starting at y inside
// the monitor spanning screenY .. screenY+screenHeight.
func KeepOnScreenY(y, height, screenY, screenHeight int32) int32 {
	bottom := screenY + screenHeight
	if y+height > bottom {
		return max(bottom-height, screenY)
	}
	return max(y, screenY)
}

// CardInWindow is the card position inside a window covering game (logical px), from the
// card's screen position (cardX, cardY) (physical px). points: the game rect is in logical
// points (macOS) rather than pixels.
func CardInWindow(cardX, cardY int32, game platform.Rect, scale float64, points bool) (x, y float64) {
	if !(scale > 0) {
		scale = 1
	}
	cx, cy := float64(cardX)/scale, float64(cardY)/scale
	gx, gy := float64(game.X), float64(game.Y)
	if !points {
		gx, gy = gx/scale, gy/scale
	}
	return rustMaxF64(cx-gx, 0), rustMaxF64(cy-gy, 0)
}

// PointInRect reports whether (px, py) lies inside the rectangle (rx, ry, rw, rh), edges
// included.
func PointInRect(px, py, rx, ry, rw, rh float64) bool {
	return px >= rx && px <= rx+rw && py >= ry && py <= ry+rh
}

// ClampInto moves the top-left (px, py) just enough that a panel of size (w, h) lies inside
// area (pinned to the left/top edge when it is larger).
func ClampInto(px, py, w, h int32, area Area) (x, y int32) {
	x = max(min(px, area.X+area.Width-w), area.X)
	y = max(min(py, area.Y+area.Height-h), area.Y)
	return x, y
}

// Intersect is the overlap of two areas (a when they do not overlap).
func Intersect(a, b Area) Area {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return a
	}
	return Area{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// rustMaxF64 is Rust's f64::max: a NaN operand yields the other one.
func rustMaxF64(a, b float64) float64 {
	switch {
	case math.IsNaN(a):
		return b
	case math.IsNaN(b):
		return a
	}
	return max(a, b)
}
