package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/data"
	"github.com/BorrowedMirror/divvy/internal/ipc"
)

func TestUnidentifiedUniquesListTheirPossibleUniques(t *testing.T) {
	gameData, err := data.Bundled()
	require.NoError(t, err)
	defaults := config.DefaultSettings()
	opts := queryOptions(&defaults, "Standard")
	text := "Item Class: Rings\nRarity: Unique\nGold Ring\n--------\nItem Level: 75\n--------\nUnidentified\n"
	_, f, err := processItemText(text, gameData, &opts)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(f.UniqueCandidates), 2, "%q", f.UniqueCandidates)
	assert.Nil(t, f.Name)
	assert.Equal(t, new("Gold Ring"), f.BaseType)
	assert.Equal(t, new(false), f.Misc.Identified)

	_, rare, err := processItemText("Item Class: Rings\nRarity: Rare\nX\nGold Ring\n", gameData, &opts)
	require.NoError(t, err)
	assert.Empty(t, rare.UniqueCandidates)
}

func TestItemTextErrorsAreParseErrors(t *testing.T) {
	defaults := config.DefaultSettings()
	opts := queryOptions(&defaults, "Standard")
	_, _, err := processItemText(strings.Repeat("x", maxItemText+1), nil, &opts)
	require.EqualError(t, err, "item text too large")
	assert.Equal(t, ipc.KindParse, ipc.From(err).Kind)

	_, _, err = processItemText("", nil, &opts)
	e, ok := errors.AsType[*itemTextError](err)
	require.True(t, ok, "%v", err)
	require.Error(t, e.err, "the parser's error is kept")
	assert.Equal(t, e.err.Error(), err.Error())
	assert.Equal(t, ipc.KindParse, ipc.From(err).Kind)
}

func TestQueryOptionsFromSettings(t *testing.T) {
	s := config.DefaultSettings()
	o := queryOptions(&s, "Dawn")
	assert.Equal(t, "Dawn", o.League)
	assert.Equal(t, core.StatusAny, o.Status)
}
