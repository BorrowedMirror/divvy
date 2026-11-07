package x11

import (
	"reflect"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/jezek/xgb/xproto"
)

func st(window xproto.Window, x int32, focused bool) *snapshotState {
	rect := platform.Rect{X: x, Width: 10, Height: 10}
	return &snapshotState{
		tracked: tracked{window: window, rect: rect, focused: focused},
		info:    platform.WindowInfo{Title: platform.GameWindowTitle, Rect: rect, Focused: focused, NativeID: uint64(window)},
	}
}

func types(evs []platform.WindowEvent) []platform.WindowEventType {
	var out []platform.WindowEventType
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func TestOnlyWatchedPropertiesTriggerRescans(t *testing.T) {
	const root, game = 1, 7
	const active, clients, netName, wmName, userTime = 10, 11, 12, 39, 99
	w := watched{root: root, rootAtoms: [2]xproto.Atom{active, clients}, game: game, titleAtoms: [2]xproto.Atom{netName, wmName}}
	prop := func(win xproto.Window, atom xproto.Atom) xproto.PropertyNotifyEvent {
		return xproto.PropertyNotifyEvent{Window: win, Atom: atom}
	}
	check := func(got, want bool, what string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: got %v", what, got)
		}
	}
	check(w.relevant(prop(root, active)), true, "root active")
	check(w.relevant(prop(root, clients)), true, "root clients")
	check(w.relevant(prop(root, userTime)), false, "root user time")
	check(w.relevant(prop(game, netName)), true, "game net name")
	check(w.relevant(prop(game, wmName)), true, "game wm name")
	check(w.relevant(prop(game, userTime)), false, "_NET_WM_USER_TIME is ignored")
	check(w.relevant(prop(3, netName)), false, "other windows' titles are ignored")
	noGame := w
	noGame.game = 0
	check(noGame.relevant(prop(game, netName)), false, "no game")
	check(w.relevant(xproto.MapNotifyEvent{}), true, "map")
	check(w.relevant(xproto.KeymapNotifyEvent{}), false, "keymap")
}

func TestDiffCoversAllTransitions(t *testing.T) {
	if len(diff(nil, nil)) != 0 {
		t.Fatal("none -> none")
	}
	if got := types(diff(st(1, 0, false), nil)); !reflect.DeepEqual(got, []platform.WindowEventType{platform.WindowLost}) {
		t.Fatalf("lost: %v", got)
	}
	if got := diff(nil, st(1, 0, false)); len(got) != 1 || got[0].Type != platform.WindowFound || got[0].Info.NativeID != 1 {
		t.Fatalf("found: %v", got)
	}
	if got := types(diff(st(1, 0, false), st(2, 0, false))); !reflect.DeepEqual(got, []platform.WindowEventType{platform.WindowLost, platform.WindowFound}) {
		t.Fatalf("replaced: %v", got)
	}
	if got := diff(st(1, 0, false), st(1, 0, false)); len(got) != 0 {
		t.Fatalf("unchanged: %v", got)
	}
	want := []platform.WindowEvent{
		{Type: platform.WindowMoved, Rect: platform.Rect{X: 5, Width: 10, Height: 10}},
		{Type: platform.WindowFocusChanged, Focused: true},
	}
	if got := diff(st(1, 0, false), st(1, 5, true)); !reflect.DeepEqual(got, want) {
		t.Fatalf("moved+focus: %v", got)
	}
}
