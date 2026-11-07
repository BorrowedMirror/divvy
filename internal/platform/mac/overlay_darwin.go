//go:build darwin

package mac

// macOS overlay styling. Wails already creates the overlay as a non-activating NSPanel
// (MacWindowClassPanel + NonActivating); this adds what the Rust tauri-nspanel setup added on
// top: status window level, all Spaces + full-screen auxiliary collection behaviour, a floating
// panel that only becomes key when needed, no shadow. Every AppKit call runs on the main thread.

import (
	"errors"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

const (
	// overlayLevel is one above the menu bar (NSStatusWindowLevel, 25): enough to sit over a
	// full-screen game without covering system UI the way screen-saver level would.
	overlayLevel = 25

	nsWindowStyleMaskNonactivatingPanel uint = 1 << 7

	// NSWindowCollectionBehavior: CanJoinAllSpaces | Stationary | IgnoresCycle |
	// FullScreenAuxiliary.
	overlayCollectionBehavior uint = 1<<0 | 1<<4 | 1<<6 | 1<<8
)

type overlay struct {
	win objc.ID
}

// newOverlay wraps a Wails window's NativeWindow() (an NSWindow*).
func newOverlay(native unsafe.Pointer) (platform.OverlayWindow, error) {
	if native == nil {
		return nil, platform.Backend("overlay: nil NSWindow")
	}
	if err := loadSys(); err != nil {
		return nil, platform.Backend("overlay: %v", err)
	}
	if err := loadAppKit(); err != nil {
		return nil, platform.Backend("overlay: %v", err)
	}
	return &overlay{win: objc.ID(uintptr(native))}, nil
}

func (o *overlay) send(selector string, args ...any) { o.win.Send(sel(selector), args...) }

func (o *overlay) isPanel() bool {
	return objc.Send[bool](o.win, sel("isKindOfClass:"), class("NSPanel"))
}

// keepOnTop reapplies the level and shadow: Wails resets the level to floating (3) and the
// shadow the first time a hidden AlwaysOnTop window becomes key.
func (o *overlay) keepOnTop() {
	o.send("setLevel:", overlayLevel)
	o.send("setHasShadow:", false)
}

// Prepare applies the panel styles. Idempotent.
func (o *overlay) Prepare() error {
	var err error
	runOnMain(func() {
		if o.isPanel() {
			// Key only when a text field is clicked, so the game keeps keyboard input otherwise.
			if mask := objc.Send[uint](o.win, sel("styleMask")); mask&nsWindowStyleMaskNonactivatingPanel == 0 {
				o.send("setStyleMask:", mask|nsWindowStyleMaskNonactivatingPanel)
			}
			o.send("setFloatingPanel:", true)
			o.send("setBecomesKeyOnlyIfNeeded:", true)
			o.send("setWorksWhenModal:", true)
		} else {
			err = errors.New("overlay is not an NSPanel; create it with MacWindowClassPanel")
		}
		o.keepOnTop()
		o.send("setCollectionBehavior:", overlayCollectionBehavior)
		o.send("setHidesOnDeactivate:", false)
	})
	if err != nil {
		return platform.Backend("%v", err)
	}
	return nil
}

// ShowAt places the overlay at rect, given in points with a top-left global origin (the space
// of the tracker's CGWindowList rects), and orders it front without activating the app.
func (o *overlay) ShowAt(rect platform.Rect) error {
	runOnMain(func() {
		// Cocoa's global origin is the bottom-left of the primary screen.
		var primaryHeight float64
		if screens := objc.ID(class("NSScreen")).Send(sel("screens")); screens != 0 &&
			objc.Send[uint](screens, sel("count")) > 0 {
			primary := screens.Send(sel("objectAtIndex:"), uint(0))
			primaryHeight = objc.Send[cgRect](primary, sel("frame")).Size.Height
		}
		frame := cgRect{Origin: cgPoint{X: float64(rect.X), Y: primaryHeight - float64(rect.Y) - float64(rect.Height)}}
		frame.Size.Width, frame.Size.Height = float64(rect.Width), float64(rect.Height)
		o.send("setFrame:display:", frame, true)
		o.keepOnTop()
		o.send("orderFrontRegardless")
	})
	return nil
}

func (o *overlay) Hide() error {
	runOnMain(func() { o.send("orderOut:", objc.ID(0)) })
	return nil
}

// SetClickThrough lets mouse events pass through the overlay to the game (or not).
func (o *overlay) SetClickThrough(enabled bool) error {
	runOnMain(func() { o.send("setIgnoresMouseEvents:", enabled) })
	return nil
}

// RaiseAbove keeps the overlay at the status level in front; on macOS that already stacks it
// over a full-screen game, so owner is not needed.
func (o *overlay) RaiseAbove(*uint64) error {
	runOnMain(func() {
		o.keepOnTop()
		o.send("orderFrontRegardless")
	})
	return nil
}

// Activate gives the overlay keyboard focus as Tauri's set_focus did: activate the app, then
// make the window key and order it front.
func (o *overlay) Activate() error {
	runOnMain(func() {
		app := objc.ID(class("NSApplication")).Send(sel("sharedApplication"))
		app.Send(sel("activateIgnoringOtherApps:"), true)
		o.send("makeKeyAndOrderFront:", objc.ID(0))
	})
	return nil
}

// cursor is the mouse position in points, global top-left origin: the same space as the window
// tracker's CGWindowList rects (not physical pixels).
func cursor() (x, y float64, ok bool) {
	if loadSys() != nil {
		return 0, 0, false
	}
	event := cgEventCreate(0)
	if event == 0 {
		return 0, 0, false
	}
	defer cfRelease(event)
	p := cgEventGetLocation(event)
	return p.X, p.Y, true
}
