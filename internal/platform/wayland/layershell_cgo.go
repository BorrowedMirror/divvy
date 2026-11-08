//go:build linux && cgo && gtk3

package wayland

/*
#cgo pkg-config: gtk+-3.0 gtk-layer-shell-0
#include "layershell.h"
*/
import "C"

import (
	"log/slog"
	"sync"
	"unsafe"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// layerOverlay styles the overlay's GtkWindow as a gtk-layer-shell surface on the OVERLAY layer
// (just window hints on GNOME, which has no layer-shell).
//
// Every method runs its GTK calls through the main-thread dispatcher ([WithMainThread]), so they
// may be called from any goroutine when that is Wails' application.InvokeSync (which runs
// directly when already on the main thread). Without a dispatcher they must be called on the
// GTK main thread.
type layerOverlay struct {
	win   *C.GtkWindow
	run   func(func())
	layer bool // false on GNOME

	mu           sync.Mutex
	clickThrough bool
}

var (
	_ platform.OverlayWindow = (*layerOverlay)(nil)
	_ platform.LayerAnchorer = (*layerOverlay)(nil)
	_ platform.Resizer       = (*layerOverlay)(nil)
)

// newOverlay wraps the GtkWindow* of a Wails window (its NativeWindow()).
func newOverlay(native unsafe.Pointer, run func(func()), gnome bool) (platform.OverlayWindow, error) {
	if native == nil {
		return nil, platform.Unsupported("overlay: expected a GtkWindow")
	}
	return &layerOverlay{win: (*C.GtkWindow)(native), run: run, layer: !gnome}, nil
}

// Prepare applies always-on-top / skip-taskbar / all-workspaces hints and (not on GNOME) turns
// the window into a layer surface. Call it once, before the window's first Show: layer-shell
// must be initialised before GTK realises the window. A realised window is hidden and
// unrealised first, so the web view is realised again with the layer surface on the next show.
// The window's size request is pinned to its current size: gtk-layer-shell sizes the surface
// from it, and a web view requests next to nothing (the surface is then never painted).
func (o *layerOverlay) Prepare() error {
	var rc C.int
	o.run(func() { rc = C.rkl_ls_prepare(o.win, cBool(o.layer)) })
	switch rc {
	case C.RKL_LS_UNSUPPORTED:
		return platform.Backend("compositor does not implement wlr-layer-shell; overlay will be a normal window")
	case C.RKL_LS_UNREALISED:
		slog.Warn("overlay window already realised; unrealised it to attach layer-shell")
	}
	return nil
}

// ShowAt shows the overlay at rect, in GDK (logical) screen coordinates. A layer surface cannot
// be positioned: it is moved to the monitor under rect's centre and anchored to that monitor's
// nearer vertical edge ([AnchorFor]).
func (o *layerOverlay) ShowAt(rect platform.Rect) error {
	o.mu.Lock()
	through := o.clickThrough
	o.mu.Unlock()
	var err error
	o.run(func() {
		if C.rkl_ls_is_layer(o.win) != 0 {
			cx, cy := rect.X+int32(rect.Width/2), rect.Y+int32(rect.Height/2)
			g := C.rkl_ls_monitor_area(o.win, C.int(cx), C.int(cy))
			area := platform.Rect{X: int32(g.x), Y: int32(g.y), Width: uint32(g.width), Height: uint32(g.height)}
			if area.Width > 0 && area.Height > 0 {
				err = o.anchor(AnchorFor(rect, area))
			}
		}
		C.rkl_ls_show(o.win, C.int(rect.X), C.int(rect.Y), C.int(rect.Width), C.int(rect.Height))
		C.rkl_ls_click_through(o.win, cBool(through))
	})
	return err
}

// Hide implements [platform.OverlayWindow].
func (o *layerOverlay) Hide() error {
	o.run(func() { C.rkl_ls_hide(o.win) })
	return nil
}

// SetClickThrough lets pointer input pass through the overlay (an empty input region). It is
// applied now if the window is realised, and again on every ShowAt.
func (o *layerOverlay) SetClickThrough(enabled bool) error {
	o.mu.Lock()
	o.clickThrough = enabled
	o.mu.Unlock()
	o.run(func() { C.rkl_ls_click_through(o.win, cBool(enabled)) })
	return nil
}

// RaiseAbove is a no-op: the layer surface already sits in the OVERLAY layer, above every
// toplevel, and Wayland gives clients no stacking control otherwise.
func (*layerOverlay) RaiseAbove(uint64) error { return nil }

// Activate is a no-op: the layer surface takes the keyboard on demand, when the user clicks
// into it, and Wayland has no focus stealing to request it otherwise.
func (*layerOverlay) Activate() error { return nil }

// AnchorLayer implements [platform.LayerAnchorer]: it places the layer surface against the right
// (or left) output edge, top and side px in (surface-local, i.e. logical, px). Positions have no
// effect on layer surfaces. It calls GTK, so it runs through the main-thread dispatcher; with
// InvokeSync that is safe from any goroutine and from the main thread itself.
func (o *layerOverlay) AnchorLayer(right bool, top, side int32) error {
	var err error
	o.run(func() { err = o.anchor(Anchor{Right: right, Top: top, Side: side}) })
	return err
}

// Resize implements [platform.Resizer]: it sizes the overlay (logical px). A layer surface
// takes its size from the window's size request, so Wails' SetSize (gtk_window_resize) alone
// does not resize it.
func (o *layerOverlay) Resize(width, height int32) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	o.run(func() { C.rkl_ls_resize(o.win, C.int(width), C.int(height)) })
	return nil
}

// anchor runs on the GTK main thread.
func (o *layerOverlay) anchor(a Anchor) error {
	if C.rkl_ls_anchor(o.win, cBool(a.Right), C.int(a.Top), C.int(a.Side)) == C.RKL_LS_NOT_LAYER {
		return platform.Backend("overlay is not a layer-shell surface")
	}
	return nil
}

// isLayer and isMapped are for tests; GTK main thread only.
func (o *layerOverlay) isLayer() bool  { return C.rkl_ls_is_layer(o.win) != 0 }
func (o *layerOverlay) isMapped() bool { return C.rkl_ls_mapped(o.win) != 0 }

func cBool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
