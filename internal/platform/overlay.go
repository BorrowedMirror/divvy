package platform

// OverlayWindow is native styling for the overlay window, implemented over a native window
// handle by each backend.
type OverlayWindow interface {
	// Prepare applies one-time native styles (no-activate, always on top, all Spaces,
	// layer shell...).
	Prepare() error
	ShowAt(rect Rect) error
	Hide() error
	SetClickThrough(enabled bool) error
	// RaiseAbove stacks the overlay directly above owner (the game window's NativeID; 0 for
	// none) and keeps it there, where the WM puts a focused fullscreen game above "always on
	// top" windows.
	RaiseAbove(owner uint64) error
	// Activate gives the overlay keyboard focus, bypassing focus-stealing prevention where the
	// platform allows it (pinned panel, settings).
	Activate() error
}

// Optional extras an [OverlayWindow] may implement; the shell checks with a type assertion and
// skips what a backend does not offer.
type (
	// Rounder gives the window rounded corners (physical px); width 0 removes the shape.
	Rounder interface {
		SetRoundedShape(width, height, radius int32) error
	}
	// DirectMover moves the window straight through the window system, bypassing the
	// toolkit's "already there" shortcut (X11).
	DirectMover interface {
		MoveDirect(x, y int32) error
	}
	// Resizer sizes the window itself (logical px) where the toolkit's resize does not reach
	// it: a Wayland layer surface takes its size from the window's size request.
	Resizer interface {
		Resize(width, height int32) error
	}
	// FocusRefuser makes the window never take keyboard focus, even when the toolkit maps
	// it (GTK rewrites the window manager's focus hints on every show). It calls the toolkit,
	// so it must run on the UI thread.
	FocusRefuser interface {
		RefuseFocus() error
	}
	// GameDetacher drops the window's link to the game window (X11 WM_TRANSIENT_FOR, set by
	// RaiseAbove), so it never points at a game window that has gone away.
	GameDetacher interface {
		DetachFromGame() error
	}
	// ToolWindow names a frameless helper window and keeps it out of taskbars and window
	// lists, through the toolkit (GTK rewrites the window manager's hints on every show, and
	// Wails gives frameless windows no title). UI thread only.
	ToolWindow interface {
		MarkToolWindow(title string) error
	}
	// LayerAnchorer anchors a layer-shell surface to an output edge (Wayland-native): right
	// selects the right edge, top and side are margins in px.
	LayerAnchorer interface {
		AnchorLayer(right bool, top, side int32) error
	}
)
