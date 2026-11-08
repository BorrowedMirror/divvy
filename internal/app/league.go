package app

import (
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

// autoLeague picks the current softcore trade league: the first league that is not Standard,
// Hardcore, SSF, Solo Self-Found or a private/event ruthless variant; falls back to `Standard`.
// ok is false when neither is listed.
func autoLeague(leagues []trade.League) (league string, ok bool) {
	challengeSoftcore := func(id string) bool {
		id = strings.ToLower(id)
		return id != "standard" &&
			!strings.Contains(id, "hardcore") &&
			!strings.HasPrefix(id, "hc ") &&
			!strings.Contains(id, "ssf") &&
			!strings.Contains(id, "solo self-found") &&
			!strings.Contains(id, "ruthless")
	}
	for _, l := range leagues {
		if challengeSoftcore(l.ID) {
			return l.ID, true
		}
	}
	for _, l := range leagues {
		if strings.EqualFold(l.ID, "standard") {
			return l.ID, true
		}
	}
	return "", false
}

// resolveLeague is the league to use given settings and the live league list (nil when it
// could not be fetched).
func resolveLeague(settings *config.Settings, live []trade.League) string {
	if !settings.League.AutoDetect && settings.League.League != nil && *settings.League.League != "" {
		return *settings.League.League
	}
	if l, ok := autoLeague(live); ok {
		return l
	}
	if settings.League.League != nil {
		return *settings.League.League
	}
	return "Standard"
}
