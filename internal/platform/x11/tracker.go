package x11

import (
	"slices"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// watched says which events should trigger a rescan.
type watched struct {
	root xproto.Window
	// rootAtoms are _NET_ACTIVE_WINDOW and _NET_CLIENT_LIST on the root.
	rootAtoms [2]xproto.Atom
	game      xproto.Window // 0 = none
	// titleAtoms are _NET_WM_NAME and WM_NAME on the game window.
	titleAtoms [2]xproto.Atom
}

// relevant: PropertyNotify only for the atoms we read (not e.g. _NET_WM_USER_TIME, which
// changes on every keypress in the game); structure changes always.
func (w *watched) relevant(ev xgb.Event) bool {
	switch e := ev.(type) {
	case xproto.PropertyNotifyEvent:
		if e.Window == w.root {
			return slices.Contains(w.rootAtoms[:], e.Atom)
		}
		return w.game != 0 && e.Window == w.game && slices.Contains(w.titleAtoms[:], e.Atom)
	case xproto.ConfigureNotifyEvent, xproto.MapNotifyEvent, xproto.UnmapNotifyEvent,
		xproto.DestroyNotifyEvent, xproto.ReparentNotifyEvent:
		return true
	}
	return false
}
