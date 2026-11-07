//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Settings are saved to RKL_CONFIG_DIR, survive a restart, and the settings page renders.
func TestSettings(t *testing.T) {
	session := NewSession(t)
	app := session.Start(t)

	steps(t,
		step{"saves settings to the isolated config dir", func(t *testing.T) {
			s := app.Settings(t)
			s["league"].(map[string]any)["auto_detect"] = false
			s["league"].(map[string]any)["league"] = "Hardcore"
			s["trade"].(map[string]any)["widen_pct"] = 17
			b, err := json.Marshal(s)
			require.NoError(t, err)
			app.OK(t, "save_settings "+string(b))

			saved := app.Settings(t)
			assert.Equal(t, "Hardcore", saved["league"].(map[string]any)["league"])
			toml, err := os.ReadFile(session.SettingsFile())
			require.NoError(t, err)
			assert.Contains(t, string(toml), "Hardcore")
			assert.Regexp(t, `widen_pct\s*=\s*17`, string(toml))
		}},
		step{"restores them after a restart", func(t *testing.T) {
			app.Quit(t)
			app = session.Start(t)
			s := app.Settings(t)
			assert.Equal(t, false, s["league"].(map[string]any)["auto_detect"])
			assert.Equal(t, "Hardcore", s["league"].(map[string]any)["league"])
			assert.EqualValues(t, 17, s["trade"].(map[string]any)["widen_pct"])
		}},
		step{"renders the settings page", func(t *testing.T) {
			app.OK(t, "tray open_settings")
			app.WaitState(t, "the settings window", 10*time.Second, func(s State) bool { return s.SettingsVisible })
			app.WaitDOM(t, "settings", `main[data-route="settings"] [data-testid="settings-shell"]`, 10*time.Second,
				func(d DOM) bool { return d.Visible })
			title := app.WaitDOM(t, "settings", `main[data-route="settings"] h1`, 10*time.Second,
				func(d DOM) bool { return d.Visible && strings.TrimSpace(d.Text) != "" })
			t.Logf("settings title: %q", title.Text)
			missing, err := app.DOM("settings", `[data-testid="no-such-element"]`)
			require.NoError(t, err)
			assert.Equal(t, DOM{}, missing)
		}},
		step{"quits cleanly", func(t *testing.T) { app.Quit(t) }},
	)
}
