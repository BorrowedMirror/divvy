//go:build linux

package x11

// Native overlay styling over an X11 window id: EWMH always-on-top / skip-taskbar / sticky,
// no input focus (WM_HINTS), click-through via an empty XFixes input shape, rounded corners via
// SHAPE, and stacking above the game.

import (
	"errors"
	"log/slog"
	"math"
	"sync"
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
)

// netWMStateAdd is _NET_WM_STATE_ADD from the EWMH spec.
const netWMStateAdd = 1

// sourcePager is the EWMH source indication for pagers, which WMs obey without applying
// focus-stealing prevention.
const sourcePager = 2

// Overlay is the X11 [platform.OverlayWindow]; it also implements [platform.Rounder] and
// [platform.DirectMover].
type Overlay struct {
	conn   *xConn
	window xproto.Window

	extOnce sync.Once
	extErr  error
}

var (
	_ platform.OverlayWindow = (*Overlay)(nil)
	_ platform.Rounder       = (*Overlay)(nil)
	_ platform.DirectMover   = (*Overlay)(nil)
)

// NewOverlay styles X window xid on display ("" = $DISPLAY) over a connection of its own
// (window ids are server-global).
func NewOverlay(display string, xid uint32) (*Overlay, error) {
	if xid == 0 {
		return nil, platform.Backend("x11: no window id")
	}
	conn, err := connect(display, nil)
	if err != nil {
		return nil, err
	}
	return &Overlay{conn: conn, window: xproto.Window(xid)}, nil
}

// overlayFromNative builds the styler for a Wails window's NativeWindow() (a GtkWindow*).
func overlayFromNative(display string, native unsafe.Pointer) (platform.OverlayWindow, error) {
	xid, err := xidFromGtk(native)
	if err != nil {
		return nil, err
	}
	return NewOverlay(display, xid)
}

// Window is the overlay's X window id.
func (o *Overlay) Window() uint32 { return uint32(o.window) }

// Close releases the overlay's connection.
func (o *Overlay) Close() { o.conn.close() }

func (o *Overlay) wmStateAtoms() [4]xproto.Atom {
	a := &o.conn.atoms
	return [4]xproto.Atom{a.netWMStateAbove, a.netWMStateSkipTaskbar, a.netWMStateSkipPager, a.netWMStateSticky}
}

// setInputHint sets the WM_HINTS input field, keeping the other hints the toolkit set.
func (o *Overlay) setInputHint(input bool) error {
	const inputHint = 1
	hints := make([]uint32, 9)
	copy(hints, o.conn.property32(o.window, xproto.AtomWmHints, xproto.AtomWmHints, 9))
	hints[0] |= inputHint
	hints[1] = 0
	if input {
		hints[1] = 1
	}
	return o.conn.changeProperty32(o.window, xproto.AtomWmHints, xproto.AtomWmHints, hints)
}

// Prepare implements [platform.OverlayWindow].
func (o *Overlay) Prepare() error {
	states := o.wmStateAtoms()
	vals := []uint32{uint32(states[0]), uint32(states[1]), uint32(states[2]), uint32(states[3])}
	// Before mapping, the WM reads _NET_WM_STATE from the property...
	if err := o.conn.changeProperty32(o.window, o.conn.atoms.netWMState, xproto.AtomAtom, vals); err != nil {
		return err
	}
	// ...after mapping it only honours client messages to the root.
	for i := 0; i < len(vals); i += 2 {
		if err := o.conn.sendRootMessage(o.window, o.conn.atoms.netWMState, [5]uint32{netWMStateAdd, vals[i], vals[i+1], 1, 0}); err != nil {
			return err
		}
	}
	// Never take keyboard focus away from the game.
	return o.setInputHint(false)
}

// ShowAt implements [platform.OverlayWindow].
func (o *Overlay) ShowAt(rect platform.Rect) error {
	mask := uint16(xproto.ConfigWindowX | xproto.ConfigWindowY | xproto.ConfigWindowWidth | xproto.ConfigWindowHeight)
	xproto.ConfigureWindow(o.conn.c, o.window, mask, []uint32{uint32(rect.X), uint32(rect.Y), rect.Width, rect.Height})
	if err := xproto.MapWindowChecked(o.conn.c, o.window).Check(); err != nil {
		return xErr(err)
	}
	return nil
}

// Hide implements [platform.OverlayWindow].
func (o *Overlay) Hide() error {
	if err := xproto.UnmapWindowChecked(o.conn.c, o.window).Check(); err != nil {
		return xErr(err)
	}
	return nil
}

// RaiseAbove implements [platform.OverlayWindow]. Together with [Overlay.Activate] it is the
// X11 part of the Rust shell's bring_to_front(window, owner, focus): RaiseAbove, then Activate
// when focus is wanted. It also asks compositors not to shadow the window, as the Rust shell
// did on every raise.
func (o *Overlay) RaiseAbove(owner *uint64) error {
	if err := o.DisableCompositorShadow(); err != nil {
		slog.Debug("disable compositor shadow", "error", err)
	}
	var sibling uint32
	if owner != nil && *owner != 0 && *owner <= math.MaxUint32 && uint32(*owner) != uint32(o.window) {
		sibling = uint32(*owner)
		// Transients are kept above their parent, including a focused fullscreen parent.
		if err := o.conn.changeProperty32(o.window, xproto.AtomWmTransientFor, xproto.AtomWindow, []uint32{sibling}); err != nil {
			return err
		}
	}
	// EWMH restack request (source 2 = pager, so WMs honour it), sibling = owner, Above.
	if err := o.conn.sendRootMessage(o.window, o.conn.atoms.netRestackWindow, [5]uint32{sourcePager, sibling, xproto.StackModeAbove, 0, 0}); err != nil {
		return err
	}
	// Fallback for WMs without _NET_RESTACK_WINDOW.
	if err := xproto.ConfigureWindowChecked(o.conn.c, o.window, xproto.ConfigWindowStackMode, []uint32{xproto.StackModeAbove}).Check(); err != nil {
		return xErr(err)
	}
	return nil
}

// Activate implements [platform.OverlayWindow].
func (o *Overlay) Activate() error {
	if err := o.setInputHint(true); err != nil {
		return err
	}
	// Source 2 (pager) bypasses focus-stealing prevention; timestamp 0 = current time.
	return o.conn.sendRootMessage(o.window, o.conn.atoms.netActiveWindow, [5]uint32{sourcePager, 0, 0, 0, 0})
}

func (o *Overlay) initExtensions() error {
	o.extOnce.Do(func() {
		if err := o.conn.useExtension("XFIXES"); err != nil {
			o.extErr = err
			return
		}
		if err := o.conn.useExtension("SHAPE"); err != nil {
			o.extErr = err
			return
		}
		if _, err := xfixes.QueryVersion(o.conn.c, 5, 0).Reply(); err != nil {
			o.extErr = xErr(err)
		}
	})
	return o.extErr
}

// SetClickThrough implements [platform.OverlayWindow]: an empty input shape lets every click
// through; clearing it restores the default (the whole window).
func (o *Overlay) SetClickThrough(enabled bool) error {
	if err := o.initExtensions(); err != nil {
		return err
	}
	c := o.conn.c
	if !enabled {
		if err := xfixes.SetWindowShapeRegionChecked(c, o.window, shape.SkInput, 0, 0, 0).Check(); err != nil {
			return xErr(err)
		}
		return nil
	}
	region, err := xfixes.NewRegionId(c)
	if err != nil {
		return xErr(err)
	}
	if err := xfixes.CreateRegionChecked(c, region, nil).Check(); err != nil {
		return xErr(err)
	}
	err = xfixes.SetWindowShapeRegionChecked(c, o.window, shape.SkInput, 0, 0, region).Check()
	err = errors.Join(err, xfixes.DestroyRegionChecked(c, region).Check())
	if err != nil {
		return xErr(err)
	}
	return nil
}

// SetRoundedShape implements [platform.Rounder]: the bounding shape becomes a rounded
// rectangle, so compositors draw their shadow around the rounded card instead of the window's
// square corners. A zero width or radius removes the shape.
func (o *Overlay) SetRoundedShape(width, height, radius int32) error {
	if err := o.initExtensions(); err != nil {
		return err
	}
	c := o.conn.c
	if width <= 0 || radius <= 0 {
		if err := shape.MaskChecked(c, shape.SoSet, shape.SkBounding, o.window, 0, 0, 0).Check(); err != nil {
			return xErr(err)
		}
		return nil
	}
	rects := roundedRectSpans(uint32(width), uint32(max(height, 0)), uint32(radius))
	if err := shape.RectanglesChecked(c, shape.SoSet, shape.SkBounding, xproto.ClipOrderingUnsorted, o.window, 0, 0, rects).Check(); err != nil {
		return xErr(err)
	}
	return nil
}

// MoveDirect implements [platform.DirectMover]: a plain configure request, which WMs such as
// openbox honour even over docks' reserved strips (unlike the placement they apply on map).
func (o *Overlay) MoveDirect(x, y int32) error {
	if err := xproto.ConfigureWindowChecked(o.conn.c, o.window, xproto.ConfigWindowX|xproto.ConfigWindowY, []uint32{uint32(x), uint32(y)}).Check(); err != nil {
		return xErr(err)
	}
	return nil
}

// DisableCompositorShadow asks X11 compositors (picom, compton) not to draw a drop shadow
// around the window: the overlay's transparent padding would otherwise show the shadow as a
// grey band around the card. Harmless without a compositor.
func (o *Overlay) DisableCompositorShadow() error {
	return o.conn.changeProperty32(o.window, o.conn.atoms.comptonShadow, xproto.AtomCardinal, []uint32{0})
}

// shared is a lazily opened connection for the package-level helpers, reopened after a failure.
var shared struct {
	sync.Mutex
	display string
	conn    *xConn
}

func withShared(display string, f func(*xConn) error) error {
	shared.Lock()
	defer shared.Unlock()
	if shared.conn == nil || shared.display != display {
		if shared.conn != nil {
			shared.conn.close()
		}
		conn, err := connect(display, nil)
		if err != nil {
			shared.conn = nil
			return err
		}
		shared.conn, shared.display = conn, display
	}
	err := f(shared.conn)
	if err != nil {
		// A broken connection is replaced on the next call; X errors cost one reconnect.
		shared.conn.close()
		shared.conn = nil
	}
	return err
}

// ActivateWindow gives keyboard focus to another client (the game) with a pager-source
// _NET_ACTIVE_WINDOW request, without touching that client's hints.
func ActivateWindow(display string, window uint64) error {
	if window == 0 || window > math.MaxUint32 {
		return platform.Backend("x11 window id out of range")
	}
	return withShared(display, func(x *xConn) error {
		return x.sendRootMessage(xproto.Window(window), x.atoms.netActiveWindow, [5]uint32{sourcePager, 0, 0, 0, 0})
	})
}

// ActiveWindow is the window the WM reports as active (_NET_ACTIVE_WINDOW), if any.
func ActiveWindow(display string) (uint64, bool, error) {
	var id uint64
	err := withShared(display, func(x *xConn) error {
		r, err := xproto.GetProperty(x.c, false, x.root, x.atoms.netActiveWindow, xproto.AtomWindow, 0, 1).Reply()
		if err != nil {
			return xErr(err)
		}
		if r != nil && r.Format == 32 && len(r.Value) >= 4 {
			id = uint64(uint32(r.Value[0]) | uint32(r.Value[1])<<8 | uint32(r.Value[2])<<16 | uint32(r.Value[3])<<24)
		}
		return nil
	})
	return id, err == nil && id != 0, err
}

// Cursor is the pointer position in root (physical) pixels. QueryPointer is answered even
// while another client (a Wine game) holds a pointer grab.
func Cursor(display string) (x, y float64, ok bool) {
	err := withShared(display, func(c *xConn) error {
		r, err := xproto.QueryPointer(c.c, c.root).Reply()
		if err != nil {
			return xErr(err)
		}
		if r == nil {
			return nil
		}
		x, y, ok = float64(r.RootX), float64(r.RootY), true
		return nil
	})
	return x, y, err == nil && ok
}
