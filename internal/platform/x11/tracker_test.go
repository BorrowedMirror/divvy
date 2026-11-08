package x11

import (
	"testing"

	"github.com/jezek/xgb/xproto"
	"github.com/stretchr/testify/assert"
)

func TestOnlyWatchedPropertiesTriggerRescans(t *testing.T) {
	const root, game = 1, 7
	const active, clients, netName, wmName, userTime = 10, 11, 12, 39, 99
	w := watched{root: root, rootAtoms: [2]xproto.Atom{active, clients}, game: game, titleAtoms: [2]xproto.Atom{netName, wmName}}
	prop := func(win xproto.Window, atom xproto.Atom) xproto.PropertyNotifyEvent {
		return xproto.PropertyNotifyEvent{Window: win, Atom: atom}
	}
	assert.True(t, w.relevant(prop(root, active)), "root active")
	assert.True(t, w.relevant(prop(root, clients)), "root clients")
	assert.False(t, w.relevant(prop(root, userTime)), "root user time")
	assert.True(t, w.relevant(prop(game, netName)), "game net name")
	assert.True(t, w.relevant(prop(game, wmName)), "game wm name")
	assert.False(t, w.relevant(prop(game, userTime)), "_NET_WM_USER_TIME is ignored")
	assert.False(t, w.relevant(prop(3, netName)), "other windows' titles are ignored")
	noGame := w
	noGame.game = 0
	assert.False(t, noGame.relevant(prop(game, netName)), "no game")
	assert.True(t, w.relevant(xproto.MapNotifyEvent{}), "map")
	assert.False(t, w.relevant(xproto.KeymapNotifyEvent{}), "keymap")
}
