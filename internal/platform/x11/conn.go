//go:build linux

package x11

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/record"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

func init() {
	// xgb logs to stderr by default (missing Xauthority, read errors on close); we report
	// failures through errors and capabilities instead.
	xgb.Logger.SetOutput(io.Discard)
}

func xErr(err error) error {
	return platform.Backend("x11: %v", err)
}

type atoms struct {
	netActiveWindow, netRestackWindow, comptonShadow, netClientList, netWMName, netWMState,
	netWMStateAbove, netWMStateSkipTaskbar, netWMStateSkipPager, netWMStateSticky, utf8String xproto.Atom
}

// xConn is an X connection plus the atoms we need.
type xConn struct {
	c     *xgb.Conn
	root  xproto.Window
	atoms atoms
}

// connect opens display ("" = $DISPLAY). X errors of unchecked requests and events arrive on
// the event queue; they go to events when it is non-nil (closed when the connection ends),
// else they are drained so the queue never fills up.
func connect(display string, events chan<- xgb.Event) (*xConn, error) {
	registerExtensions(display)
	c, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, xErr(err)
	}
	go func() {
		if events != nil {
			defer close(events)
		}
		for {
			ev, xerr := c.WaitForEvent()
			if ev == nil && xerr == nil {
				return
			}
			if ev != nil && events != nil {
				events <- ev
			}
		}
	}()
	x := &xConn{c: c, root: xproto.Setup(c).DefaultScreen(c).Root}
	names := []string{"_NET_ACTIVE_WINDOW", "_NET_RESTACK_WINDOW", "_COMPTON_SHADOW", "_NET_CLIENT_LIST",
		"_NET_WM_NAME", "_NET_WM_STATE", "_NET_WM_STATE_ABOVE", "_NET_WM_STATE_SKIP_TASKBAR",
		"_NET_WM_STATE_SKIP_PAGER", "_NET_WM_STATE_STICKY", "UTF8_STRING"}
	targets := []*xproto.Atom{&x.atoms.netActiveWindow, &x.atoms.netRestackWindow, &x.atoms.comptonShadow,
		&x.atoms.netClientList, &x.atoms.netWMName, &x.atoms.netWMState, &x.atoms.netWMStateAbove,
		&x.atoms.netWMStateSkipTaskbar, &x.atoms.netWMStateSkipPager, &x.atoms.netWMStateSticky, &x.atoms.utf8String}
	cookies := make([]xproto.InternAtomCookie, len(names))
	for i, n := range names {
		cookies[i] = xproto.InternAtom(c, false, uint16(len(n)), n)
	}
	for i, ck := range cookies {
		r, err := ck.Reply()
		if err != nil || r == nil {
			c.Close()
			return nil, xErr(fmt.Errorf("intern %s: %v", names[i], err))
		}
		*targets[i] = r.Atom
	}
	return x, nil
}

func (x *xConn) close() { x.c.Close() }

// xgb's extension Init writes process-global event/error tables that every connection's reader
// goroutine reads without a lock, so calling it while other connections are live is a data
// race. registerExtensions runs each Init once, on a private connection opened before any
// other; afterwards connections only record the opcode ([xConn.useExtension]).
var extRegistry struct {
	sync.Mutex
	done bool
}

func registerExtensions(display string) {
	extRegistry.Lock()
	defer extRegistry.Unlock()
	if extRegistry.done {
		return
	}
	c, err := xgb.NewConnDisplay(display)
	if err != nil {
		return // retried with the next connection
	}
	defer c.Close()
	// Each Init fails harmlessly when the server lacks the extension.
	_ = record.Init(c)
	_ = xtest.Init(c)
	_ = shape.Init(c)
	_ = xfixes.Init(c)
	extRegistry.done = true
}

// useExtension makes extension name usable on this connection (the opcode lookup xgb's
// generated requests need), failing when the server lacks it.
func (x *xConn) useExtension(name string) error {
	r, err := xproto.QueryExtension(x.c, uint16(len(name)), name).Reply()
	if err != nil {
		return xErr(err)
	}
	if r == nil || !r.Present {
		return platform.Backend("x11: the X server lacks the %s extension", name)
	}
	x.c.ExtLock.Lock()
	x.c.Extensions[name] = r.MajorOpcode
	x.c.ExtLock.Unlock()
	return nil
}

func (x *xConn) hasExtension(name string) bool {
	r, err := xproto.QueryExtension(x.c, uint16(len(name)), name).Reply()
	return err == nil && r != nil && r.Present
}

// property32 reads a format-32 property as uint32s.
func (x *xConn) property32(w xproto.Window, prop, typ xproto.Atom, length uint32) []uint32 {
	r, err := xproto.GetProperty(x.c, false, w, prop, typ, 0, length).Reply()
	if err != nil || r == nil || r.Format != 32 {
		return nil
	}
	out := make([]uint32, 0, r.ValueLen)
	for i := 0; i+4 <= len(r.Value); i += 4 {
		out = append(out, xgb.Get32(r.Value[i:]))
	}
	return out
}

// title is _NET_WM_NAME (UTF-8), falling back to ICCCM WM_NAME.
func (x *xConn) title(w xproto.Window) (string, bool) {
	get := func(prop, typ xproto.Atom) (string, bool) {
		r, err := xproto.GetProperty(x.c, false, w, prop, typ, 0, 1024).Reply()
		if err != nil || r == nil || len(r.Value) == 0 {
			return "", false
		}
		s := string(r.Value)
		if !utf8.ValidString(s) {
			s = strings.ToValidUTF8(s, "�")
		}
		return s, true
	}
	if t, ok := get(x.atoms.netWMName, x.atoms.utf8String); ok {
		return t, true
	}
	return get(xproto.AtomWmName, xproto.AtomString)
}

func (x *xConn) isGame(w xproto.Window) bool {
	t, ok := x.title(w)
	return ok && t == platform.GameWindowTitle
}

// findGame finds the game window: EWMH client list first, then a shallow tree walk (no WM, or a
// WM without _NET_CLIENT_LIST; depth 2 covers reparenting frames).
func (x *xConn) findGame() (xproto.Window, bool) {
	for _, w := range x.property32(x.root, x.atoms.netClientList, xproto.AtomWindow, 4096) {
		if x.isGame(xproto.Window(w)) {
			return xproto.Window(w), true
		}
	}
	return x.walk(x.root, 2)
}

func (x *xConn) walk(parent xproto.Window, depth int) (xproto.Window, bool) {
	tree, err := xproto.QueryTree(x.c, parent).Reply()
	if err != nil || tree == nil {
		return 0, false
	}
	for i := len(tree.Children) - 1; i >= 0; i-- {
		child := tree.Children[i]
		attrs, err := xproto.GetWindowAttributes(x.c, child).Reply()
		if err != nil || attrs == nil || attrs.MapState != xproto.MapStateViewable {
			continue
		}
		if x.isGame(child) {
			return child, true
		}
		if depth > 1 {
			if w, ok := x.walk(child, depth-1); ok {
				return w, true
			}
		}
	}
	return 0, false
}

// active is _NET_ACTIVE_WINDOW, falling back to the input focus (no EWMH WM).
func (x *xConn) active() (xproto.Window, bool) {
	if v := x.property32(x.root, x.atoms.netActiveWindow, xproto.AtomWindow, 1); len(v) > 0 && v[0] != 0 {
		return xproto.Window(v[0]), true
	}
	r, err := xproto.GetInputFocus(x.c).Reply()
	if err != nil || r == nil || r.Focus <= 1 {
		return 0, false
	}
	return r.Focus, true
}

// isFocused reports whether w (or a descendant of it) is focused.
func (x *xConn) isFocused(w xproto.Window) bool {
	cur, ok := x.active()
	for range 8 {
		switch {
		case !ok:
			return false
		case cur == w:
			return true
		case cur != x.root && cur != 0:
			tree, err := xproto.QueryTree(x.c, cur).Reply()
			if err != nil || tree == nil {
				return false
			}
			cur = tree.Parent
		default:
			return false
		}
	}
	return false
}

// rect is the window geometry in root coordinates.
func (x *xConn) rect(w xproto.Window) (platform.Rect, bool) {
	geo, err := xproto.GetGeometry(x.c, xproto.Drawable(w)).Reply()
	if err != nil || geo == nil {
		return platform.Rect{}, false
	}
	pos, err := xproto.TranslateCoordinates(x.c, w, x.root, 0, 0).Reply()
	if err != nil || pos == nil {
		return platform.Rect{}, false
	}
	return platform.Rect{X: int32(pos.DstX), Y: int32(pos.DstY), Width: uint32(geo.Width), Height: uint32(geo.Height)}, true
}

func (x *xConn) info(w xproto.Window) (platform.WindowInfo, bool) {
	title, ok := x.title(w)
	if !ok {
		return platform.WindowInfo{}, false
	}
	rect, ok := x.rect(w)
	if !ok {
		return platform.WindowInfo{}, false
	}
	return platform.WindowInfo{Title: title, Rect: rect, Focused: x.isFocused(w), NativeID: uint64(w)}, true
}

// sendRootMessage sends an EWMH client message about w to the root window.
func (x *xConn) sendRootMessage(w xproto.Window, typ xproto.Atom, data [5]uint32) error {
	ev := xproto.ClientMessageEvent{Format: 32, Window: w, Type: typ, Data: xproto.ClientMessageDataUnionData32New(data[:])}
	mask := uint32(xproto.EventMaskSubstructureRedirect | xproto.EventMaskSubstructureNotify)
	if err := xproto.SendEventChecked(x.c, false, x.root, mask, string(ev.Bytes())).Check(); err != nil {
		return xErr(err)
	}
	return nil
}

func (x *xConn) changeProperty32(w xproto.Window, prop, typ xproto.Atom, values []uint32) error {
	buf := make([]byte, 4*len(values))
	for i, v := range values {
		xgb.Put32(buf[4*i:], v)
	}
	if err := xproto.ChangePropertyChecked(x.c, xproto.PropModeReplace, w, prop, typ, 32, uint32(len(values)), buf).Check(); err != nil {
		return xErr(err)
	}
	return nil
}
