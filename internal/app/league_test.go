package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/trade"
)

func testLeague(id string) trade.League {
	return trade.League{ID: id, Realm: new("poe2"), Text: new(id)}
}

func TestAutoLeaguePrefersChallengeSoftcore(t *testing.T) {
	testCases := []struct {
		name     string
		have     []trade.League
		expected string
		ok       bool
	}{
		{
			"PicksChallengeSoftcore",
			[]trade.League{testLeague("Standard"), testLeague("Hardcore"), testLeague("Rise of the Abyssal"), testLeague("HC Rise of the Abyssal")},
			"Rise of the Abyssal",
			true,
		},
		{
			"FallsBackToStandard",
			[]trade.League{testLeague("Standard"), testLeague("Hardcore")},
			"Standard",
			true,
		},
		{
			"NoLeagues",
			nil,
			"",
			false,
		},
		{
			"SkipsSoloSelfFound",
			[]trade.League{testLeague("SSF Standard")},
			"",
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := autoLeague(tc.have)
			assert.Equal(t, tc.expected, got)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func TestResolveLeagueRespectsManualSetting(t *testing.T) {
	s := config.DefaultSettings()
	s.League.AutoDetect = false
	s.League.League = new("Standard")
	dawn := []trade.League{testLeague("Dawn")}
	assert.Equal(t, "Standard", resolveLeague(&s, dawn), "manual")
	s.League.AutoDetect = true
	assert.Equal(t, "Dawn", resolveLeague(&s, dawn), "auto")
	assert.Equal(t, "Standard", resolveLeague(&s, nil), "offline")
	s.League.League = nil
	assert.Equal(t, "Standard", resolveLeague(&s, nil), "offline, nothing saved")
}
