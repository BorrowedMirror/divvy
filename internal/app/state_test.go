package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

func newTestState(t *testing.T, gameData func() (*data.GameData, error)) *State {
	t.Helper()
	if gameData == nil {
		gameData = func() (*data.GameData, error) { return nil, errors.New("no data in this test") }
	}
	st, err := NewState(nil, config.DefaultSettings(), nil, platform.BackendX11, gameData)
	require.NoError(t, err)
	return st
}

func league(id string) trade.League { return trade.League{ID: id} }

func TestStateAutoDetectDoesNotOverwriteNewerLeagues(t *testing.T) {
	st := newTestState(t, nil)
	st.SetLeagues([]trade.League{league("Fresh")})
	st.SetLeaguesIfUnset([]trade.League{league("Stale")})
	assert.Equal(t, "Fresh", st.League())

	empty := newTestState(t, nil)
	empty.SetLeaguesIfUnset([]trade.League{league("Dawn")})
	assert.Equal(t, "Dawn", empty.League())
}

func TestStateDataIsAbsentUntilDecoded(t *testing.T) {
	st := newTestState(t, nil)
	assert.Nil(t, st.Data())
	assert.Nil(t, st.WaitData(), "a failed decode unblocks waiters")
	assert.Nil(t, st.Data())
}

func TestStateUpdatedSourcesKeepUnchangedClients(t *testing.T) {
	s := config.DefaultSettings()
	base, err := SourcesFromSettings(&s)
	require.NoError(t, err)
	s.League.League = new("Dawn")
	s.Trade.WidenPct = 25
	next, err := base.Updated(&s)
	require.NoError(t, err)
	assert.Nil(t, next, "unrelated change keeps everything")

	s.Trade.Poesessid = new("abc")
	next, err = base.Updated(&s)
	require.NoError(t, err)
	require.NotNil(t, next, "poesessid rebuilds trade")
	assert.NotSame(t, base.Trade, next.Trade)
	assert.Same(t, base.Ninja, next.Ninja)
	assert.Same(t, base.Scout, next.Scout)
	assert.Equal(t, "abc", next.Trade.Config().POESESSID)

	s.Trade.Contact = new("me@example.com")
	ua, err := next.Updated(&s)
	require.NoError(t, err)
	require.NotNil(t, ua, "user agent rebuilds all")
	assert.NotSame(t, next.Trade, ua.Trade)
	assert.NotSame(t, next.Ninja, ua.Ninja)
	again, err := ua.Updated(&s)
	require.NoError(t, err)
	assert.Nil(t, again)
}

func TestStatePanelPositionRoundTripsAndToleratesBadFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", PositionFile)
	_, _, ok := LoadPosition(path)
	assert.False(t, ok)
	require.NoError(t, SavePosition(path, 2600, -40))
	x, y, ok := LoadPosition(path)
	assert.True(t, ok)
	assert.Equal(t, [2]int32{2600, -40}, [2]int32{x, y})
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o644))
	_, _, ok = LoadPosition(path)
	assert.False(t, ok)
}

func TestStateKeepsTheLastFiftyErrors(t *testing.T) {
	st := newTestState(t, nil)
	for i := range 60 {
		st.RecordError(i)
	}
	errs := st.RecentErrors()
	assert.Len(t, errs, maxErrors)
	assert.Equal(t, "10", errs[0])
	assert.Equal(t, "59", errs[len(errs)-1])
}

func TestEndpointOverridesNeedATestHookBuild(t *testing.T) {
	t.Setenv("RKL_TRADE_BASE_URL", "http://127.0.0.1:9")
	_, ok := EnvOverride("RKL_TRADE_BASE_URL")
	assert.Equal(t, testHookBuild, ok)
	t.Setenv("RKL_TRADE_BASE_URL", "http://example.com")
	_, ok = EnvOverride("RKL_TRADE_BASE_URL")
	assert.False(t, ok, "non-loopback http is never allowed")
}
