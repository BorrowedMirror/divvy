//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tray menu. Tray hosts are not scriptable in CI, so items are run through the test hook
// (`tray <item>`), which calls the same handlers as a click.
func TestTray(t *testing.T) {
	app := newSession(t).Start(t)

	steps(t,
		step{"installs a tray icon", func(t *testing.T) {
			s, err := app.State()
			require.NoError(t, err)
			assert.True(t, s.TrayPresent, "state: %s", s)
		}},
		step{"rejects unknown menu items", func(t *testing.T) {
			r, err := app.Command("tray does-not-exist")
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(r, "err "), "reply: %s", r)
		}},
		step{"'Settings' opens the settings window", func(t *testing.T) {
			app.OK(t, "tray open_settings")
			app.WaitState(t, "the settings window", 10*time.Second, func(s appState) bool { return s.SettingsVisible })
		}},
		// Last: the app is gone afterwards.
		step{"'Quit' exits the app", func(t *testing.T) {
			app.OK(t, "tray quit")
			app.WaitExit(t, 10*time.Second)
		}},
	)
}
