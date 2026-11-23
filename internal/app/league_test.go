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
	for _, c := range []struct {
		leagues []trade.League
		want    string
		ok      bool
	}{
		{[]trade.League{testLeague("Standard"), testLeague("Hardcore"), testLeague("Rise of the Abyssal"), testLeague("HC Rise of the Abyssal")}, "Rise of the Abyssal", true},
		{[]trade.League{testLeague("Standard"), testLeague("Hardcore")}, "Standard", true},
		{nil, "", false},
		{[]trade.League{testLeague("SSF Standard")}, "", false},
	} {
		got, ok := autoLeague(c.leagues)
		assert.Equal(t, c.want, got)
		assert.Equal(t, c.ok, ok)
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
