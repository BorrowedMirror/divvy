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

	"golang.org/x/sys/windows"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// gwlExStyle is GWL_EXSTYLE; a variable so the negative index converts to uintptr
// (sign-extended) at run time.
var gwlExStyle int32 = -20

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
const hwndTopmost = ^uintptr(0)

type overlay struct {
	hwnd         windows.HWND
	addedLayered atomic.Bool
}

var _ platform.OverlayWindow = (*overlay)(nil)

func (o *overlay) window() (windows.HWND, error) {
	if !windows.IsWindow(o.hwnd) {
		return 0, platform.Backend("overlay HWND %#x is not a window", uintptr(o.hwnd))
	}
	return o.hwnd, nil
}

func exStyle(hwnd windows.HWND) uint32 {
	// SAFETY: hwnd was validated; reading GWL_EXSTYLE has no side effects. The value is a
	// 32-bit style mask stored in a LONG_PTR.
	r, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(gwlExStyle))
	return uint32(r)
}

func setExStyle(hwnd windows.HWND, style uint32) error {
	// SAFETY: hwnd is a window of this process; GWL_EXSTYLE takes a style mask. The previous
	// value is not an error indicator.
	procSetWindowLongPtrW.Call(uintptr(hwnd), uintptr(gwlExStyle), uintptr(style))
	// SAFETY: SWP_FRAMECHANGED applies the new styles without moving, sizing or activating.
	if ok, _, err := procSetWindowPos.Call(uintptr(hwnd), hwndTopmost, 0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged); ok == 0 {
		return platform.Backend("SetWindowPos: %v", err)
	}
	return nil
}

func (o *overlay) Prepare() error {
	hwnd, err := o.window()
	if err != nil {
		return err
	}
	return setExStyle(hwnd, preparedExStyle(exStyle(hwnd)))
}

func (o *overlay) ShowAt(rect platform.Rect) error {
	hwnd, err := o.window()
	if err != nil {
		return err
	}
	w := int32(min(rect.Width, math.MaxInt32))
	h := int32(min(rect.Height, math.MaxInt32))
	// SAFETY: valid HWND; HWND_TOPMOST + SWP_NOACTIVATE keeps the game focused. Signed
	// coordinates are passed sign-extended, as the C ABI expects for int.
	if ok, _, err := procSetWindowPos.Call(uintptr(hwnd), hwndTopmost, uintptr(rect.X), uintptr(rect.Y),
		uintptr(w), uintptr(h), swpNoActivate|swpShowWindow); ok == 0 {
		return platform.Backend("SetWindowPos: %v", err)
	}
	// SAFETY: valid HWND. The return value is the previous visibility, not an error.
	procShowWindow.Call(uintptr(hwnd), swShowNoActivate)
	return nil
}

func (o *overlay) Hide() error {
	hwnd, err := o.window()
	if err != nil {
		return err
	}
	// SAFETY: valid HWND. The return value is the previous visibility, not an error.
	procShowWindow.Call(uintptr(hwnd), swHide)
	return nil
}

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
		// SAFETY: valid HWND that now has WS_EX_LAYERED.
		if ok, _, err := procSetLayeredWindowAttributes.Call(uintptr(hwnd), 0, 255, lwaAlpha); ok == 0 {
			return platform.Backend("SetLayeredWindowAttributes: %v", err)
		}
	}
	return nil
}

// RaiseAbove is a no-op: the overlay is WS_EX_TOPMOST, which on Windows stays above a focused
// borderless game (the Rust backend used the trait's no-op default too).
func (o *overlay) RaiseAbove(*uint64) error { return nil }

// Activate is a no-op, as in the Rust backend (the trait's default).
func (o *overlay) Activate() error { return nil }
