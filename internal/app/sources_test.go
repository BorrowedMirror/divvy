package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/trade"
)

func TestUserAgentAppendsContact(t *testing.T) {
	s := config.DefaultSettings()
	assert.Equal(t, trade.DefaultUserAgent(Version), userAgent(&s), "no contact by default")
	s.Trade.Contact = new("me@example.com")
	assert.Equal(t, trade.DefaultUserAgent(Version)+" me@example.com", userAgent(&s))
}

func TestUpdatedSourcesKeepUnchangedClients(t *testing.T) {
	s := config.DefaultSettings()
	base, err := newPriceSources(&s)
	require.NoError(t, err)
	s.League.League = new("Dawn")
	s.Trade.WidenPct = 25
	next, err := base.updated(&s)
	require.NoError(t, err)
	assert.Same(t, base, next, "an unrelated change keeps everything")

	s.Trade.POESESSID = new("abc")
	next, err = base.updated(&s)
	require.NoError(t, err)
	assert.NotSame(t, base.trade, next.trade, "POESESSID rebuilds the trade client")
	assert.Same(t, base.ninja, next.ninja)
	assert.Same(t, base.scout, next.scout)
	assert.Equal(t, "abc", next.trade.Config().POESESSID)

	s.Trade.Contact = new("me@example.com")
	ua, err := next.updated(&s)
	require.NoError(t, err)
	assert.NotSame(t, next.trade, ua.trade, "the user agent rebuilds all")
	assert.NotSame(t, next.ninja, ua.ninja)
	again, err := ua.updated(&s)
	require.NoError(t, err)
	assert.Same(t, ua, again)
}

func TestEndpointOverridesNeedATestHookBuild(t *testing.T) {
	t.Setenv("DIVVY_TRADE_BASE_URL", "http://127.0.0.1:9")
	_, ok := envOverride("DIVVY_TRADE_BASE_URL")
	assert.Equal(t, testHookBuild, ok)
	t.Setenv("DIVVY_TRADE_BASE_URL", "http://example.com")
	_, ok = envOverride("DIVVY_TRADE_BASE_URL")
	assert.False(t, ok, "non-loopback http is never allowed")
}
