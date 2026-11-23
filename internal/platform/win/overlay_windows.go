package win

// Overlay styling on a raw HWND.
//
// Prepare adds WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW | WS_EX_TOPMOST so showing or clicking the
// overlay never steals focus from the game and it has no taskbar button. Click-through adds
// WS_EX_LAYERED | WS_EX_TRANSPARENT (layered with full opacity, as required for transparent
// hit-testing). All calls are safe from any thread of the owning process.

import (
	"math"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// gwlExStyle is GWL_EXSTYLE (-20), sign-extended to the uintptr GetWindowLongPtrW takes: the
// two's complement of 20 is ^(20 - 1).
const gwlExStyle = ^uintptr(20 - 1)

const (
	swHide           = 0
	swShowNoActivate = 4

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040

	lwaAlpha = 0x2
)

// hwndTopmost is HWND_TOPMOST, (HWND)-1.
const hwndTopmost = windows.HWND(^uintptr(0))

type overlay struct {
	hwnd         windows.HWND
	addedLayered atomic.Bool
}

var _ platform.OverlayWindow = (*overlay)(nil)

func newOverlay(native unsafe.Pointer) (platform.OverlayWindow, error) {
	hwnd, err := hwndFrom(native)
	if err != nil {
		return nil, err
	}
	return &overlay{hwnd: windows.HWND(hwnd)}, nil
}

func (o *overlay) window() (windows.HWND, error) {
	if !windows.IsWindow(o.hwnd) {
		return 0, platform.Backend("overlay HWND %#x is not a window", uintptr(o.hwnd))
	}
	return o.hwnd, nil
}

// exStyle reads GWL_EXSTYLE, a 32-bit style mask stored in a LONG_PTR.
func exStyle(hwnd windows.HWND) uint32 {
	r, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), gwlExStyle)
	return uint32(r)
}

func setExStyle(hwnd windows.HWND, style uint32) error {
	// The previous value it returns is not an error indicator.
	procSetWindowLongPtrW.Call(uintptr(hwnd), gwlExStyle, uintptr(style))
	// SWP_FRAMECHANGED applies the new styles without moving, sizing or activating.
	if err := setWindowPos(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged); err != nil {
		return platform.Backend("SetWindowPos: %w", err)
	}
	return nil
}

// Prepare implements [platform.OverlayWindow].
func (o *overlay) Prepare() error {
	hwnd, err := o.window()
	if err != nil {
		return err
	}
	return setExStyle(hwnd, preparedExStyle(exStyle(hwnd)))
}

// ShowAt implements [platform.OverlayWindow]: HWND_TOPMOST and SWP_NOACTIVATE keep the game
// focused.
func (o *overlay) ShowAt(rect platform.Rect) error {
	hwnd, err := o.window()
	if err != nil {
		return err
	}
	w := int32(min(rect.Width, math.MaxInt32))
	h := int32(min(rect.Height, math.MaxInt32))
	if err := setWindowPos(hwnd, hwndTopmost, rect.X, rect.Y, w, h, swpNoActivate|swpShowWindow); err != nil {
		return platform.Backend("SetWindowPos: %w", err)
	}
	showWindow(hwnd, swShowNoActivate) // returns the previous visibility, not an error
	return nil
}

// Hide implements [platform.OverlayWindow].
func (o *overlay) Hide() error {
	hwnd, err := o.window()
	if err != nil {
		return err
	}
	showWindow(hwnd, swHide)
	return nil
}

// SetClickThrough implements [platform.OverlayWindow].
func (o *overlay) SetClickThrough(enabled bool) error {
	hwnd, err := o.window()
	if err != nil {
		return err
	}
	weAdded := o.addedLayered.Load()
	style, nowAdded := clickThroughExStyle(exStyle(hwnd), enabled, weAdded)
	if err := setExStyle(hwnd, style); err != nil {
		return err
	}
	o.addedLayered.Store(nowAdded)
	if enabled && nowAdded && !weAdded {
		// A freshly layered window is invisible until its attributes are set: full opacity.
		// Not done when the owner made it layered (it may use UpdateLayeredWindow).
		if err := setLayeredWindowAttributes(hwnd, 0, 255, lwaAlpha); err != nil {
			return platform.Backend("SetLayeredWindowAttributes: %w", err)
		}
	}
	return nil
}

// RaiseAbove is a no-op: the overlay is WS_EX_TOPMOST, which on Windows stays above a focused
// borderless game.
func (*overlay) RaiseAbove(uint64) error { return nil }

// Activate is a no-op: the shell focuses the window through Wails, and a forced
// SetForegroundWindow can be refused by the foreground lock.
func (*overlay) Activate() error { return nil }
