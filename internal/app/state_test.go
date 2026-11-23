package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/data"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/trade"
)

// newTestState builds an X11 state on the bundled game data, without a platform or settings
// directory.
func newTestState(t *testing.T) *State {
	t.Helper()
	st, err := NewState(StateOptions{Settings: config.DefaultSettings(), Backend: platform.BackendX11, GameData: data.Bundled})
	require.NoError(t, err)
	return st
}

func TestStateLeagueComesFromTheLiveList(t *testing.T) {
	st := newTestState(t)
	assert.Equal(t, "Standard", st.league())
	st.setLeagues([]trade.League{{ID: "Dawn"}})
	assert.Equal(t, "Dawn", st.league())
}

func TestStateDataIsAbsentUntilDecoded(t *testing.T) {
	st, err := NewState(StateOptions{Settings: config.DefaultSettings()})
	require.NoError(t, err)
	assert.Nil(t, st.data())
	assert.Nil(t, st.waitData(), "a failed decode unblocks waiters")
	assert.Nil(t, st.data())
	assert.Nil(t, st.dataVersion())
}

func TestStateKeepsTheLastFiftyErrors(t *testing.T) {
	st := newTestState(t)
	for i := range 60 {
		st.RecordError(strconv.Itoa(i), nil)
	}
	st.RecordError("save", errors.New("disk full"))
	errs := st.recentErrors()
	assert.Len(t, errs, maxErrors)
	assert.Equal(t, "11", errs[0])
	assert.Equal(t, "save: disk full", errs[len(errs)-1])
}

func TestStateLastItemAndPrice(t *testing.T) {
	st := newTestState(t)
	_, ok := st.lastItem()
	assert.False(t, ok)
	assert.Empty(t, st.lastItemText())
	_, ok = st.lastTradeURL()
	assert.False(t, ok)
	assert.Nil(t, st.lastPriceSummary())

	st.setLastItem(copiedItem{text: "Rarity: Rare"})
	assert.Equal(t, "Rarity: Rare", st.lastItemText())
	st.setLastPrice(core.PriceSummary{TradeURL: new("https://www.pathofexile.com/trade2/search/poe2/Standard/x")})
	u, ok := st.lastTradeURL()
	assert.True(t, ok)
	assert.Equal(t, "https://www.pathofexile.com/trade2/search/poe2/Standard/x", u)
	require.NotNil(t, st.lastPriceSummary())
}

func TestStateDroppedPositionIsSaved(t *testing.T) {
	paths := config.Under(t.TempDir())
	st, err := NewState(StateOptions{Paths: &paths, Settings: config.DefaultSettings()})
	require.NoError(t, err)
	_, ok := st.droppedPosition()
	assert.False(t, ok)
	st.setDroppedPosition(point{-5, 7})
	again, err := NewState(StateOptions{Paths: &paths, Settings: config.DefaultSettings()})
	require.NoError(t, err)
	p, ok := again.droppedPosition()
	assert.True(t, ok)
	assert.Equal(t, point{-5, 7}, p)
}

// Concurrent searches remember their filters without racing on the shared maps or the file;
// run with -race.
func TestStateRemembersFiltersConcurrently(t *testing.T) {
	paths := config.Under(t.TempDir())
	st, err := NewState(StateOptions{Paths: &paths, Settings: config.DefaultSettings()})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 20 {
				st.rememberFilters(testFilters("accessory.ring", statChoice{fmt.Sprintf("s%d", j), (i+j)%2 == 0}))
				st.applyFilterMemory(testFilters("accessory.ring", statChoice{"s1", true}))
			}
		})
	}
	wg.Wait()
	assert.Empty(t, st.recentErrors())
	path := filepath.Join(paths.CacheDir, filterMemoryFile)
	st.mu.Lock()
	want := st.filters
	st.mu.Unlock()
	assert.Equal(t, want, loadFilterMemory(path), "the file holds the last state")

	require.NoError(t, st.clearFilterMemory())
	assert.NoFileExists(t, path)
	require.NoError(t, st.clearFilterMemory(), "clearing twice")
}
