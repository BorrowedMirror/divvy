package shell

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// window adapts a Wails window to app.Window. Positions and sizes in app.Window are physical
// pixels unless the method says logical; Wails' SetSize and SetBounds take DIPs (logical px,
// points on macOS).
type window struct {
	w *application.WebviewWindow
	// resizer, when set, also sizes the window natively (Wayland layer surfaces).
	resizer platform.Resizer
}

func (x window) Show()                { x.w.Show() }
func (x window) Hide()                { x.w.Hide() }
func (x window) Focus()               { x.w.Focus() }
func (x window) IsVisible() bool      { return x.w.IsVisible() }
func (x window) IsFocused() bool      { return x.w.IsFocused() }
func (x window) ExecJS(js string)     { x.w.ExecJS(js) }
func (x window) SetZoom(zoom float64) { x.w.SetZoom(zoom) }
func (x window) Center()              { x.w.Center() }

func (x window) SetIgnoreMouseEvents(ignore bool) { x.w.SetIgnoreMouseEvents(ignore) }
func (x window) SetAlwaysOnTop(on bool)           { x.w.SetAlwaysOnTop(on) }
func (x window) SetFrameless(frameless bool)      { x.w.SetFrameless(frameless) }

// SetSkipTaskbar is a no-op: Wails sets taskbar visibility when a window is created (the
// overlay is created hidden from the taskbar, the settings window is not).
func (x window) SetSkipTaskbar(bool) {}

func (x window) ScaleFactor() float64 {
	s, err := x.w.GetScreen()
	if err != nil || s == nil || s.ScaleFactor <= 0 {
		return 1
	}
	return float64(s.ScaleFactor)
}

func (x window) Bounds() (int32, int32, int32, int32) {
	r := x.w.PhysicalBounds()
	return int32(r.X), int32(r.Y), int32(r.Width), int32(r.Height)
}

func (x window) SetPosition(px, py int32) {
	r := x.w.PhysicalBounds()
	r.X, r.Y = int(px), int(py)
	x.w.SetPhysicalBounds(r)
}

func (x window) SetSize(w, h int32) {
	r := x.w.PhysicalBounds()
	r.Width, r.Height = int(w), int(h)
	x.w.SetPhysicalBounds(r)
}

func (x window) SetLogicalBounds(px, py, w, h float64) {
	x.w.SetBounds(application.Rect{X: int(px), Y: int(py), Width: int(w + 0.5), Height: int(h + 0.5)})
}

func (x window) SetLogicalSize(w, h float64) {
	x.w.SetSize(int(w+0.5), int(h+0.5))
	if x.resizer != nil {
		if err := x.resizer.Resize(int32(w+0.5), int32(h+0.5)); err != nil {
			slog.Debug("resize layer surface", "err", err)
		}
	}
}

// Desktop implements app.Desktop over the Wails application, with native window styling from
// the OS backend's Parts.
type Desktop struct {
	shell *Shell
	parts platform.Parts
	// Relabel updates the tray menu's language.
	Relabel func(language string)

	mu      sync.Mutex
	stylers map[*application.WebviewWindow]platform.OverlayWindow
}

// NewDesktop wires the shell's windows to the backend's styling.
func NewDesktop(s *Shell, parts platform.Parts) *Desktop {
	return &Desktop{shell: s, parts: parts, stylers: map[*application.WebviewWindow]platform.OverlayWindow{}}
}

var _ app.Desktop = (*Desktop)(nil)

// Overlay is the price panel window.
func (d *Desktop) Overlay() app.Window {
	w := window{w: d.shell.Overlay}
	d.mu.Lock()
	if r, ok := d.stylers[d.shell.Overlay].(platform.Resizer); ok {
		w.resizer = r
	}
	d.mu.Unlock()
	return w
}

// Settings is the settings window.
func (d *Desktop) Settings() app.Window { return window{w: d.shell.Settings} }

// Styler is the backend's native styling for w, created on first use.
func (d *Desktop) Styler(w *application.WebviewWindow) (platform.OverlayWindow, error) {
	d.mu.Lock()
	cached, ok := d.stylers[w]
	d.mu.Unlock()
	if ok {
		return cached, nil
	}
	if d.parts.Overlay == nil {
		return nil, platform.Unsupported("overlay styling")
	}
	// Wails creates the native window from a goroutine after Run starts.
	for i := 0; w.NativeWindow() == nil; i++ {
		if i == 100 {
			return nil, platform.Backend("native window not created after 2s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Linux backends read the GtkWindow, which GTK only allows on its main thread. The lock is
	// not held across the hop: the main thread may be asking for a styler too.
	var (
		s   platform.OverlayWindow
		err error
	)
	application.InvokeSync(func() { s, err = d.parts.Overlay(w.NativeWindow()) })
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.stylers[w]; ok {
		return prev, nil // created concurrently; keep the first
	}
	d.stylers[w] = s
	return s, nil
}

func (d *Desktop) styler(w app.Window) (platform.OverlayWindow, error) {
	x, ok := w.(window)
	if !ok {
		return nil, fmt.Errorf("not a Wails window: %T", w)
	}
	return d.Styler(x.w)
}

// CursorPosition comes from the OS backend: Wails has no global cursor query.
func (d *Desktop) CursorPosition() (float64, float64, bool) {
	if d.parts.Cursor == nil {
		return 0, 0, false
	}
	return d.parts.Cursor()
}

// MonitorAt is the monitor nearest a physical point, as a physical rectangle.
func (d *Desktop) MonitorAt(x, y float64) (app.Area, bool) {
	s := d.shell.App.Screen.ScreenNearestPhysicalPoint(application.Point{X: int(x), Y: int(y)})
	if s == nil {
		s = d.shell.App.Screen.GetPrimary()
	}
	if s == nil {
		return app.Area{}, false
	}
	r := s.PhysicalBounds
	return app.Area{X: int32(r.X), Y: int32(r.Y), Width: int32(r.Width), Height: int32(r.Height)}, true
}

// Emit sends an event to both web views. Payloads marshal through their MarshalJSON, which
// writes serde's exact JSON.
func (d *Desktop) Emit(event string, payload any) { d.shell.App.Event.Emit(event, payload) }

// OpenURL opens url in the default browser.
func (d *Desktop) OpenURL(url string) error { return d.shell.App.Browser.OpenURL(url) }

// BringToFront stacks w above the game and, with focus, gives it keyboard focus.
func (d *Desktop) BringToFront(w app.Window, gameID *uint64, focus bool) error {
	s, err := d.styler(w)
	if err != nil {
		if focus {
			w.Focus()
		}
		if errors.Is(err, platform.ErrUnsupported) {
			return nil
		}
		return err
	}
	if err := s.RaiseAbove(gameID); err != nil {
		return err
	}
	if !focus {
		return nil
	}
	w.Focus()
	return s.Activate()
}

// RefocusGame hands keyboard focus back to the game.
func (d *Desktop) RefocusGame(gameID *uint64) {
	if gameID == nil || d.parts.ActivateWindow == nil {
		return
	}
	if err := d.parts.ActivateWindow(*gameID); err != nil {
		slog.Debug("refocus game", "err", err)
	}
}

// RoundWindow rounds w's corners where the backend can.
func (d *Desktop) RoundWindow(w app.Window, width, height, radius int32) {
	s, err := d.styler(w)
	if err != nil {
		return
	}
	if r, ok := s.(platform.Rounder); ok {
		if err := r.SetRoundedShape(width, height, radius); err != nil {
			slog.Debug("round window", "err", err)
		}
	}
}

// MoveDirect moves w through the window system where the backend can, else normally.
func (d *Desktop) MoveDirect(w app.Window, x, y int32) error {
	if s, err := d.styler(w); err == nil {
		if m, ok := s.(platform.DirectMover); ok {
			return m.MoveDirect(x, y)
		}
	}
	w.SetPosition(x, y)
	return nil
}

// AnchorLayer anchors a layer-shell window (Wayland-native). Layer-shell calls GTK, which
// must happen on the UI thread.
func (d *Desktop) AnchorLayer(w app.Window, right bool, top, side int32) error {
	s, err := d.styler(w)
	if err != nil {
		return err
	}
	a, ok := s.(platform.LayerAnchorer)
	if !ok {
		return nil
	}
	var anchorErr error
	application.InvokeSync(func() { anchorErr = a.AnchorLayer(right, top, side) })
	return anchorErr
}

// RelabelTray updates the tray menu's language.
func (d *Desktop) RelabelTray(language string) {
	if d.Relabel != nil {
		d.Relabel(language)
	}
}
