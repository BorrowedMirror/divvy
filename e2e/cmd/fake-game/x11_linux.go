//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// X keysyms the game looks at.
const (
	xkC, xkCUpper, xkV, xkVUpper = 0x63, 0x43, 0x76, 0x56
	xkAltL, xkAltR, xkMetaL      = 0xffe9, 0xffea, 0xffe7
	xkSuperL, xkSuperR           = 0xffeb, 0xffec
	// evdev keycodes (+8) of the physical C and V keys.
	xKeycodeC, xKeycodeV = 54, 55
)

type x11Game struct {
	g     *game
	c     *xgb.Conn
	win   xproto.Window
	gc    xproto.Gcontext
	w, h  uint16
	evs   chan xgb.Event
	atoms struct {
		clipboard, targets, utf8, text, plain, plainUTF8, netWMName, wmProtocols, wmDelete, netWMPID, paste xproto.Atom
	}
	// keyboard mapping
	minCode    xproto.Keycode
	perCode    int
	keysyms    []xproto.Keysym
	altMask    uint16
	superMask  uint16
	owned      bool
	ownedText  string
	pendingEvs []xgb.Event
}

func runX11(g *game, w, h uint32) error {
	xgb.Logger.SetOutput(io.Discard)
	if w > math.MaxUint16 || h > math.MaxUint16 {
		return fmt.Errorf("size %dx%d is too large for X11", w, h)
	}
	c, err := xgb.NewConn()
	if err != nil {
		return fmt.Errorf("x11: %w (is DISPLAY set?)", err)
	}
	defer c.Close()
	x := &x11Game{g: g, c: c, w: uint16(w), h: uint16(h), evs: make(chan xgb.Event, 64)}
	if err := x.setup(); err != nil {
		return err
	}
	go func() {
		defer close(x.evs)
		for {
			ev, xerr := c.WaitForEvent()
			switch {
			case ev == nil && xerr == nil:
				return
			case xerr != nil:
				g.debugf("x11 error: %v", xerr)
			default:
				x.evs <- ev
			}
		}
	}()
	return x.loop()
}

func (x *x11Game) intern(name string) (xproto.Atom, error) {
	r, err := xproto.InternAtom(x.c, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("x11: interning %s: %w", name, err)
	}
	return r.Atom, nil
}

func (x *x11Game) setup() error {
	for _, a := range []struct {
		dst  *xproto.Atom
		name string
	}{
		{&x.atoms.clipboard, "CLIPBOARD"}, {&x.atoms.targets, "TARGETS"}, {&x.atoms.utf8, "UTF8_STRING"},
		{&x.atoms.text, "TEXT"}, {&x.atoms.plain, "text/plain"}, {&x.atoms.plainUTF8, "text/plain;charset=utf-8"},
		{&x.atoms.netWMName, "_NET_WM_NAME"}, {&x.atoms.wmProtocols, "WM_PROTOCOLS"},
		{&x.atoms.wmDelete, "WM_DELETE_WINDOW"}, {&x.atoms.netWMPID, "_NET_WM_PID"},
		{&x.atoms.paste, "RKL_FAKE_GAME_PASTE"},
	} {
		atom, err := x.intern(a.name)
		if err != nil {
			return err
		}
		*a.dst = atom
	}
	scr := xproto.Setup(x.c).DefaultScreen(x.c)
	win, err := xproto.NewWindowId(x.c)
	if err != nil {
		return fmt.Errorf("x11: %w", err)
	}
	x.win = win
	mask := uint32(xproto.EventMaskExposure | xproto.EventMaskKeyPress | xproto.EventMaskKeyRelease |
		xproto.EventMaskStructureNotify | xproto.EventMaskFocusChange | xproto.EventMaskPropertyChange)
	err = xproto.CreateWindowChecked(x.c, scr.RootDepth, win, scr.Root, 0, 0, x.w, x.h, 0,
		xproto.WindowClassInputOutput, scr.RootVisual, xproto.CwBackPixel|xproto.CwEventMask,
		[]uint32{x.g.color(), mask}).Check()
	if err != nil {
		return fmt.Errorf("x11: creating the window: %w", err)
	}
	prop := func(p, typ xproto.Atom, format byte, data []byte) {
		xproto.ChangeProperty(x.c, xproto.PropModeReplace, win, p, typ, format, uint32(len(data))/uint32(format/8), data)
	}
	prop(xproto.AtomWmName, xproto.AtomString, 8, []byte(title))
	prop(x.atoms.netWMName, x.atoms.utf8, 8, []byte(title))
	prop(xproto.AtomWmClass, xproto.AtomString, 8, []byte(appName+"\x00"+appName+"\x00"))
	prop(x.atoms.wmProtocols, xproto.AtomAtom, 32, le32(uint32(x.atoms.wmDelete)))
	prop(x.atoms.netWMPID, xproto.AtomCardinal, 32, le32(uint32(os.Getpid())))
	x.gc, err = xproto.NewGcontextId(x.c)
	if err != nil {
		return fmt.Errorf("x11: %w", err)
	}
	if err := xproto.CreateGCChecked(x.c, x.gc, xproto.Drawable(win), xproto.GcForeground, []uint32{x.g.color()}).Check(); err != nil {
		return fmt.Errorf("x11: creating a GC: %w", err)
	}
	if err := x.loadKeymap(); err != nil {
		return err
	}
	return xproto.MapWindowChecked(x.c, win).Check()
}

func le32(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }

// loadKeymap fetches the keycode -> keysym table and which modifier bits Alt and Super are on.
func (x *x11Game) loadKeymap() error {
	setup := xproto.Setup(x.c)
	x.minCode = setup.MinKeycode
	km, err := xproto.GetKeyboardMapping(x.c, setup.MinKeycode, byte(setup.MaxKeycode-setup.MinKeycode+1)).Reply()
	if err != nil {
		return fmt.Errorf("x11: keyboard mapping: %w", err)
	}
	x.perCode, x.keysyms = int(km.KeysymsPerKeycode), km.Keysyms
	mm, err := xproto.GetModifierMapping(x.c).Reply()
	if err != nil {
		return fmt.Errorf("x11: modifier mapping: %w", err)
	}
	x.altMask, x.superMask = xproto.ModMask1, xproto.ModMask4
	per := int(mm.KeycodesPerModifier)
	var alt, super uint16
	for mod := 3; mod < 8; mod++ { // Mod1..Mod5
		for i := range per {
			for _, ks := range x.syms(mm.Keycodes[mod*per+i]) {
				switch ks {
				case xkAltL, xkAltR, xkMetaL:
					alt |= 1 << mod
				case xkSuperL, xkSuperR:
					super |= 1 << mod
				}
			}
		}
	}
	if alt != 0 {
		x.altMask = alt
	}
	if super != 0 {
		x.superMask = super
	}
	return nil
}

// syms are the first two keysyms (unshifted, shifted) of a keycode.
func (x *x11Game) syms(code xproto.Keycode) []xproto.Keysym {
	if code == 0 || code < x.minCode || x.perCode == 0 {
		return nil
	}
	i := int(code-x.minCode) * x.perCode
	if i >= len(x.keysyms) {
		return nil
	}
	return x.keysyms[i:min(i+2, i+x.perCode, len(x.keysyms))]
}

func (x *x11Game) keyEvent(code xproto.Keycode, state uint16, pressed bool) keyEvent {
	ev := keyEvent{
		pressed: pressed,
		isC:     code == xKeycodeC,
		isV:     code == xKeycodeV,
		mods: mods{
			ctrl:  state&xproto.ModMaskControl != 0,
			alt:   state&x.altMask != 0,
			shift: state&xproto.ModMaskShift != 0,
			super: state&x.superMask != 0,
		},
	}
	syms := x.syms(code)
	for _, ks := range syms {
		switch ks {
		case xkC, xkCUpper:
			ev.isC = true
		case xkV, xkVUpper:
			ev.isV = true
		}
	}
	var first xproto.Keysym
	if len(syms) > 0 {
		first = syms[0]
	}
	ev.code = keysymName(uint32(first), uint32(code))
	return ev
}

// keysymName is winit's KeyCode name for a key (X keysym, X/xkb keycode), as far as the tests
// care.
func keysymName(ks, code uint32) string {
	if ks < 0x80 {
		if n := charCode(rune(ks)); n != "" {
			return n
		}
	}
	switch ks {
	case 0x20:
		return "Space"
	case 0xff0d:
		return "Enter"
	case 0xff1b:
		return "Escape"
	case 0xff09:
		return "Tab"
	case 0xff08:
		return "Backspace"
	case 0xffe3:
		return "ControlLeft"
	case 0xffe4:
		return "ControlRight"
	case 0xffe1:
		return "ShiftLeft"
	case 0xffe2:
		return "ShiftRight"
	case xkAltL:
		return "AltLeft"
	case xkAltR:
		return "AltRight"
	}
	return fmt.Sprintf("Unidentified(Xkb(%d))", code)
}

func (x *x11Game) draw() {
	xproto.ChangeGC(x.c, x.gc, xproto.GcForeground, []uint32{x.g.color()})
	xproto.PolyFillRectangle(x.c, xproto.Drawable(x.win), x.gc, []xproto.Rectangle{{Width: x.w, Height: x.h}})
	x.c.Sync()
	x.g.drawn()
}

// next is the next event: queued ones first (set aside while waiting for a paste), else read.
func (x *x11Game) next() (xgb.Event, bool) {
	if len(x.pendingEvs) > 0 {
		ev := x.pendingEvs[0]
		x.pendingEvs = x.pendingEvs[1:]
		return ev, true
	}
	ev, ok := <-x.evs
	return ev, ok
}

func (x *x11Game) loop() error {
	for {
		ev, ok := x.next()
		if !ok {
			return errors.New("x11: connection closed")
		}
		x.g.debugf("x11 event %T %v", ev, ev)
		switch e := ev.(type) {
		case xproto.ExposeEvent:
			if e.Count == 0 {
				x.draw()
			}
		case xproto.ConfigureNotifyEvent:
			if e.Window == x.win {
				x.w, x.h = e.Width, e.Height
			}
		case xproto.MappingNotifyEvent:
			if e.Request != xproto.MappingPointer {
				if err := x.loadKeymap(); err != nil {
					return err
				}
			}
		case xproto.KeyPressEvent:
			x.key(x.keyEvent(e.Detail, e.State, true), e.Time)
		case xproto.KeyReleaseEvent:
			x.key(x.keyEvent(e.Detail, e.State, false), e.Time)
		case xproto.SelectionRequestEvent:
			x.serve(e)
		case xproto.SelectionClearEvent:
			if e.Selection == x.atoms.clipboard {
				x.owned = false
			}
		case xproto.ClientMessageEvent:
			if e.Type == x.atoms.wmProtocols && xproto.Atom(e.Data.Data32[0]) == x.atoms.wmDelete {
				return nil
			}
		case xproto.DestroyNotifyEvent:
			if e.Window == x.win {
				return nil
			}
		}
	}
}

// x11Clipboard is the CLIPBOARD selection as of one key event (its timestamp claims ownership).
type x11Clipboard struct {
	x    *x11Game
	time xproto.Timestamp
}

func (x *x11Game) key(ev keyEvent, t xproto.Timestamp) {
	if x.g.key(ev, x11Clipboard{x, t}) {
		x.draw()
	}
}

func (cb x11Clipboard) setText(text string) error {
	x := cb.x
	if err := xproto.SetSelectionOwnerChecked(x.c, x.win, x.atoms.clipboard, cb.time).Check(); err != nil {
		return err
	}
	r, err := xproto.GetSelectionOwner(x.c, x.atoms.clipboard).Reply()
	if err != nil {
		return err
	}
	if r.Owner != x.win {
		return errors.New("x11: could not take the CLIPBOARD selection")
	}
	x.owned, x.ownedText = true, text
	return nil
}

// text reads the CLIPBOARD as UTF8_STRING (our own text when we own it).
func (cb x11Clipboard) text() (string, error) {
	x := cb.x
	if x.owned {
		return x.ownedText, nil
	}
	xproto.ConvertSelection(x.c, x.win, x.atoms.clipboard, x.atoms.utf8, x.atoms.paste, cb.time)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-x.evs:
			if !ok {
				return "", errors.New("x11: connection closed")
			}
			n, isNotify := ev.(xproto.SelectionNotifyEvent)
			if !isNotify || n.Requestor != x.win || n.Selection != x.atoms.clipboard {
				x.pendingEvs = append(x.pendingEvs, ev)
				continue
			}
			if n.Property == xproto.AtomNone {
				return "", errors.New("x11: the clipboard owner refused UTF8_STRING")
			}
			r, err := xproto.GetProperty(x.c, true, x.win, x.atoms.paste, xproto.GetPropertyTypeAny, 0, math.MaxUint32/4).Reply()
			if err != nil {
				return "", err
			}
			return string(r.Value), nil
		case <-deadline:
			return "", errors.New("x11: no reply from the clipboard owner")
		}
	}
}

// serve answers a SelectionRequest for CLIPBOARD: TARGETS and the text targets.
func (x *x11Game) serve(e xproto.SelectionRequestEvent) {
	prop := e.Property
	if prop == xproto.AtomNone { // obsolete clients
		prop = e.Target
	}
	ok := false
	if e.Selection == x.atoms.clipboard && x.owned {
		textTargets := []xproto.Atom{x.atoms.utf8, x.atoms.plainUTF8, x.atoms.text, x.atoms.plain, xproto.AtomString}
		switch e.Target {
		case x.atoms.targets:
			var data []byte
			for _, a := range append([]xproto.Atom{x.atoms.targets}, textTargets...) {
				data = append(data, le32(uint32(a))...)
			}
			xproto.ChangeProperty(x.c, xproto.PropModeReplace, e.Requestor, prop, xproto.AtomAtom, 32, uint32(len(data)/4), data)
			ok = true
		default:
			for _, t := range textTargets {
				if e.Target != t {
					continue
				}
				typ := t
				if t == x.atoms.text {
					typ = x.atoms.utf8
				}
				data := []byte(x.ownedText)
				xproto.ChangeProperty(x.c, xproto.PropModeReplace, e.Requestor, prop, typ, 8, uint32(len(data)), data)
				ok = true
			}
		}
	}
	notify := xproto.SelectionNotifyEvent{
		Time: e.Time, Requestor: e.Requestor, Selection: e.Selection, Target: e.Target, Property: prop,
	}
	if !ok {
		notify.Property = xproto.AtomNone
	}
	x.g.debugf("x11 selection request target %d -> %v", e.Target, ok)
	xproto.SendEvent(x.c, false, e.Requestor, xproto.EventMaskNoEvent, string(notify.Bytes()))
	x.c.Sync()
}
