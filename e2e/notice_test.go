//go:build e2e

package e2e

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadyNotice checks the notice RKL shows over the game window when it finds it: it
// appears without taking the game's focus, then goes away by itself.
func TestReadyNotice(t *testing.T) {
	if runtime.GOOS == "linux" && !isX11() {
		t.Skip("Wayland-native cannot track the game window, so there is no ready notice")
	}
	session := newSession(t)
	app := session.Start(t)
	app.WaitOverlayLoaded(t)
	app.WaitDOM(t, "notice", `main[data-route="notice"]`, timeout(), func(d domReply) bool { return d.Exists })
	game := startGame(t, fixture("rare-ring.txt"))
	defer game.Stop()

	steps(t,
		step{"shows over the game when the game window appears", func(t *testing.T) {
			app.WaitState(t, "the ready notice", 10*time.Second, func(s appState) bool { return s.NoticeVisible })
			d := app.WaitDOM(t, "notice", `[data-testid="ready-notice"]`, 5*time.Second, func(d domReply) bool {
				return d.Visible && strings.Contains(d.Text, "Rust King Ledger")
			})
			assert.Contains(t, d.Text, "Ready and running in the background")
		}},
		step{"leaves the keyboard focus with the game", func(t *testing.T) {
			if !isX11() {
				t.Skip("checked with xdotool on X11")
			}
			out, err := shell(t, "xdotool getactivewindow getwindowname").CombinedOutput()
			require.NoError(t, err, "%s", out)
			assert.Equal(t, "Path of Exile 2", strings.TrimSpace(string(out)))
		}},
		step{"hides by itself", func(t *testing.T) {
			app.WaitState(t, "the ready notice hidden", 10*time.Second, func(s appState) bool { return !s.NoticeVisible })
		}},
	)
}
