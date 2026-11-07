//go:build e2e

package e2e

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hotkey (or hook / signal) -> the app sends the copy chord to the fake game -> clipboard ->
// parse -> mock trade2 search + fetch -> overlay with the price.
func TestPriceCheck(t *testing.T) {
	mock.Reset()
	mock.Configure(MockConfig{Listings: []MockListing{
		{Amount: 1, Currency: "divine"}, {Amount: 2, Currency: "divine"}, {Amount: 3, Currency: "divine"},
	}, Total: 3})
	session := NewSession(t)
	app := session.Start(t)
	app.WaitOverlayLoaded(t)
	game := StartGame(t, fixture("rare-ring.txt"))
	t.Logf("trigger: %s", triggerMode())

	steps(t,
		step{"starts with the overlay and the settings window hidden", func(t *testing.T) {
			s, err := app.State()
			require.NoError(t, err)
			assert.False(t, s.OverlayVisible, "overlay visible before any price check: %s", s)
			assert.False(t, s.SettingsVisible, "settings visible at a minimised start: %s", s)
		}},
		step{"copies the item from the game and shows the trade price", func(t *testing.T) {
			TriggerPriceCheck(t, app, false)
			game.WaitCopied(t, 1, 10*time.Second, func() string { s, _ := app.State(); return "state: " + s.String() })

			s := app.WaitState(t, "the overlay with a price", timeout(), func(s State) bool {
				return s.OverlayVisible && s.LastPrice != nil
			})
			require.NotNil(t, s.LastPrice.Median, "state: %s", s)
			assert.Equal(t, Price{Amount: 2, Currency: "divine"}, *s.LastPrice.Median)
			assert.Equal(t, 3, s.LastPrice.Total)
			assert.True(t, mock.Hit(http.MethodPost, "/api/trade2/search/poe2/"), "no trade2 search: %+v", mock.Requests())
			assert.True(t, mock.Hit(http.MethodGet, "/api/trade2/fetch/"), "no trade2 fetch: %+v", mock.Requests())
		}},
		step{"renders the price in the overlay", func(t *testing.T) {
			app.WaitDOM(t, "overlay", `[data-testid="price-summary"]`, 10*time.Second, func(d DOM) bool { return d.Visible })
			median := app.WaitDOM(t, "overlay", `[data-testid="price-median"]`, 10*time.Second, func(d DOM) bool {
				return d.Visible && strings.TrimSpace(d.Text) != ""
			})
			assert.Regexp(t, regexp.MustCompile(`^2(\.0+)?\b`), strings.TrimSpace(median.Text))
		}},
		step{"a pinned price check keeps the overlay open", func(t *testing.T) {
			// Searches are cached by query, so the same ring again would never reach the mock:
			// hover a different ring.
			// Close the unpinned panel first, as a player does (Esc): while it is open it keeps the
			// focus, so the game would never see the copy chord.
			app.OK(t, "hide")
			app.WaitState(t, "the overlay hidden", 10*time.Second, func(s State) bool { return !s.OverlayVisible })
			game.Stop()
			game = StartGame(t, fixture("rare-ring-2.txt"))
			mock.Reset()
			mock.Configure(MockConfig{Listings: []MockListing{{Amount: 5, Currency: "exalted"}}, Total: 1})
			TriggerPriceCheck(t, app, true)
			game.WaitCopied(t, 1, 10*time.Second, func() string { s, _ := app.State(); return "state: " + s.String() })
			s := app.WaitState(t, "the pinned overlay with the new price", timeout(), func(s State) bool {
				return s.OverlayVisible && s.LastPrice != nil && s.LastPrice.Median != nil && s.LastPrice.Median.Amount == 5
			})
			assert.Equal(t, Price{Amount: 5, Currency: "exalted"}, *s.LastPrice.Median)
			assert.True(t, s.OverlayPinned, "state: %s", s)
			assert.True(t, mock.Hit(http.MethodPost, "/api/trade2/search/poe2/"), "no trade2 search: %+v", mock.Requests())
			median := app.WaitDOM(t, "overlay", `[data-testid="price-median"]`, 10*time.Second, func(d DOM) bool {
				return d.Visible && strings.HasPrefix(strings.TrimSpace(d.Text), "5")
			})
			assert.Regexp(t, regexp.MustCompile(`^5(\.0+)?\b`), strings.TrimSpace(median.Text))
		}},
	)
}
