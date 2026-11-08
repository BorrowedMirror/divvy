//go:build linux && cgo && gtk3 && waylandtest

package wayland

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// waitFor iterates the GTK main loop until cond holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		gtkTestIterate()
		time.Sleep(10 * time.Millisecond)
	}
	gtkTestIterate()
}

// inTree reports whether sway lists a window titled title as a toplevel (layer surfaces are not
// in the tree).
func inTree(t *testing.T, title string) bool {
	t.Helper()
	gtkTestIterate()
	return strings.Contains(swaymsg(t, "get_tree"), `"name": "`+title+`"`)
}

// layerShellScenario turns GTK windows into layer surfaces, both before realisation (a Wails
// window created hidden) and after (one already shown: hide + unrealise first).
func layerShellScenario(t *testing.T, parts platform.Parts) {
	t.Helper()
	require.True(t, gtkTestInit(), "gtk_init on the headless sway display")

	// Control: a plain toplevel shows up in sway's tree.
	plain := gtkTestWindow("rkl-plain")
	gtkTestShow(plain)
	waitFor(t, "plain toplevel in tree", func() bool { return inTree(t, "rkl-plain") })

	// The virtual keyboard's chords reach the focused client with their modifiers.
	if vk, ok := parts.InputSimulator.(*virtualKeyboard); ok && vk.ready() {
		gtkTestWatchKeys(plain)
		require.NoError(t, parts.InputSimulator.SendChord(platform.NewChord("KeyC", platform.Ctrl)))
		waitFor(t, "Ctrl+C delivered", func() bool { n, ctrl, _, _ := gtkTestLastKey(); return n == "c" && ctrl })
		require.NoError(t, parts.InputSimulator.SendChord(platform.NewChord("F5", platform.Ctrl, platform.Shift)))
		// (Ctrl+Alt+F5 would be XF86Switch_VT_5 in the default keymap.)
		waitFor(t, "Ctrl+Shift+F5 delivered", func() bool {
			n, ctrl, shift, alt := gtkTestLastKey()
			return n == "F5" && ctrl && shift && !alt
		})
		require.NoError(t, parts.InputSimulator.SendChord(platform.NewChord("KeyV")))
		waitFor(t, "bare v delivered", func() bool { n, ctrl, _, _ := gtkTestLastKey(); return n == "v" && !ctrl })
	}

	// Unrealised window, like Wails' Hidden overlay: Prepare, then show.
	ov := gtkTestWindow("rkl-layer")
	require.False(t, gtkTestRealized(ov))
	w, err := parts.Overlay(ov)
	require.NoError(t, err)
	lo := w.(*layerOverlay)
	require.NoError(t, w.Prepare())
	assert.True(t, lo.isLayer())
	require.NoError(t, w.Prepare(), "idempotent")
	require.NoError(t, w.SetClickThrough(true))
	require.NoError(t, w.ShowAt(platform.Rect{X: 1000, Y: 100, Width: 300, Height: 200}))
	waitFor(t, "layer surface mapped", lo.isMapped)
	assert.False(t, inTree(t, "rkl-layer"), "a layer surface is not a toplevel")
	require.NoError(t, w.SetClickThrough(false))
	anchorer, ok := w.(platform.LayerAnchorer)
	require.True(t, ok)
	require.NoError(t, anchorer.AnchorLayer(false, 10, 20))
	require.NoError(t, w.RaiseAbove(0))
	require.NoError(t, w.Activate())
	require.NoError(t, w.Hide())
	waitFor(t, "layer surface unmapped", func() bool { return !lo.isMapped() })

	// Realised and shown first (as if Wails had shown it): Prepare hides and unrealises it.
	late := gtkTestWindow("rkl-late")
	gtkTestShow(late)
	waitFor(t, "late toplevel in tree", func() bool { return inTree(t, "rkl-late") })
	w2, err := parts.Overlay(late)
	require.NoError(t, err)
	require.NoError(t, w2.Prepare())
	assert.False(t, gtkTestRealized(late), "unrealised to attach layer-shell")
	lo2 := w2.(*layerOverlay)
	assert.True(t, lo2.isLayer())
	gtkTestShow(late) // Wails' show: realize + show_all
	waitFor(t, "late layer surface mapped", lo2.isMapped)
	waitFor(t, "late window gone from tree", func() bool { return !inTree(t, "rkl-late") })
	assert.True(t, gtkTestHasGdkWindow(late))

	// A window that is not a layer surface cannot be anchored.
	w3, err := parts.Overlay(plain)
	require.NoError(t, err)
	assert.Error(t, w3.(platform.LayerAnchorer).AnchorLayer(true, 0, 0))
}
