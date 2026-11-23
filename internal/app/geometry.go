package app

import (
	"math"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/platform"
)

// Area is an integer rectangle in physical pixels.
type Area struct {
	X, Y, Width, Height int32
}

// point is a screen position in physical pixels.
type point struct{ X, Y int32 }

const (
	// cardWidth is the price card's width (logical px), matching the overlay window's size.
	cardWidth = 420.0
	// cardHeight is the card height before the web view has measured it (logical px).
	cardHeight = 560.0
	// cardRadius is the card's corner radius (logical px), matching the CSS.
	cardRadius = 12.0
	// minOverlayHeight is the smallest overlay height allowed (logical px), so the header and
	// empty state always fit.
	minOverlayHeight = 120.0
	// overlayScreenMargin is the gap kept between the overlay and the top/bottom of the monitor
	// (logical px).
	overlayScreenMargin = 8.0
)

// overlayPosition places a panel of size (w, h) beside the cursor (cx, cy), flipping to the
// left of the cursor when it would overflow the right edge of screen, and clamping vertically.
func overlayPosition(cx, cy, w, h int32, screen Area) (x, y int32) {
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

// panelPosition is the card position for the `overlay.side` setting: beside the cursor, or
// against the left/right edge of area (the game window when tracked, else the monitor),
// vertically centred on the cursor and clamped like the cursor placement.
func panelPosition(side config.OverlaySide, cx, cy, w, h int32, area Area) (x, y int32) {
	const edge = 8
	x, y = overlayPosition(cx, cy, w, h, area)
	switch side {
	case config.SideLeft:
		return area.X + edge, y
	case config.SideRight:
		return max(area.X+area.Width-w-edge, area.X), y
	default: // SideCursor, SideLast
		return x, y
	}
}

// fitOverlayHeight is the height (logical px) to give the overlay for content of wanted px on a
// monitor that is monitorHeight logical px tall: as tall as the content, never taller than the
// screen. A non-finite wanted height gets the minimum; an unknown (NaN) monitor height allows
// only the minimum.
func fitOverlayHeight(wanted, monitorHeight float64) float64 {
	if math.IsNaN(wanted) || math.IsInf(wanted, 0) {
		return minOverlayHeight
	}
	hi := max(monitorHeight-2*overlayScreenMargin, minOverlayHeight)
	if math.IsNaN(hi) {
		hi = minOverlayHeight
	}
	return min(max(math.Ceil(wanted), minOverlayHeight), hi)
}

// keepOnScreen is the start edge (physical px, either axis) that keeps a window of size
// starting at pos inside the monitor spanning start .. start+extent.
func keepOnScreen(pos, size, start, extent int32) int32 {
	end := start + extent
	if pos+size > end {
		return max(end-size, start)
	}
	return max(pos, start)
}

// cardInWindow is the card position inside a window covering game (logical px), from the
// card's screen position (cardX, cardY) (physical px). points: the game rect is in logical
// points (macOS) rather than pixels.
func cardInWindow(cardX, cardY int32, game platform.Rect, scale float64, points bool) (x, y float64) {
	if !(scale > 0) {
		scale = 1
	}
	cx, cy := float64(cardX)/scale, float64(cardY)/scale
	gx, gy := float64(game.X), float64(game.Y)
	if !points {
		gx, gy = gx/scale, gy/scale
	}
	return max(cx-gx, 0), max(cy-gy, 0)
}

// pointInRect reports whether (px, py) lies inside the rectangle (rx, ry, rw, rh), edges
// included.
func pointInRect(px, py, rx, ry, rw, rh float64) bool {
	return px >= rx && px <= rx+rw && py >= ry && py <= ry+rh
}

// clampInto moves the top-left (px, py) just enough that a panel of size (w, h) lies inside
// area (pinned to the left/top edge when it is larger).
func clampInto(px, py, w, h int32, area Area) (x, y int32) {
	x = max(min(px, area.X+area.Width-w), area.X)
	y = max(min(py, area.Y+area.Height-h), area.Y)
	return x, y
}

// intersect is the overlap of two areas (a when they do not overlap).
func intersect(a, b Area) Area {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return a
	}
	return Area{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// cardBounds is where a card may sit over the game: the whole game window, docks' strips
// included (like the fullscreen game itself).
func cardBounds(game platform.Rect) Area {
	return Area{X: game.X, Y: game.Y, Width: int32(game.Width), Height: int32(game.Height)}
}

// physical converts logical px to physical px at scale.
func physical(v, scale float64) int32 { return int32(math.Round(v * scale)) }
