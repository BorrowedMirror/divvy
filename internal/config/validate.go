package config

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// modifierKeyCodes are `KeyboardEvent.code` names (and common aliases) that are modifiers,
// not chord keys.
var modifierKeyCodes = []string{
	"Control", "ControlLeft", "ControlRight", "Ctrl",
	"Alt", "AltLeft", "AltRight", "AltGraph", "Option",
	"Shift", "ShiftLeft", "ShiftRight",
	"Meta", "MetaLeft", "MetaRight", "OSLeft", "OSRight", "Cmd", "Super", "Win",
}

func validateChord(field string, chord platform.Chord) error {
	key := strings.TrimSpace(chord.Key)
	if key == "" {
		// An explicitly cleared hotkey ("Not set"); modifiers alone are not a chord.
		if len(chord.Modifiers) == 0 {
			return nil
		}
		return invalid(field, "chord has no key")
	}
	for _, m := range modifierKeyCodes {
		if strings.EqualFold(m, key) {
			return invalidf(field, "`%s` is a modifier; a chord needs a non-modifier key", key)
		}
	}
	return nil
}

// modifierOrder is the canonical order of chord modifiers.
var modifierOrder = []platform.KeyModifier{platform.Ctrl, platform.Alt, platform.Shift, platform.Meta}

// rank is m's position in [modifierOrder]; unknown modifiers sort last.
func rank(m platform.KeyModifier) int {
	if i := slices.Index(modifierOrder, m); i >= 0 {
		return i
	}
	return len(modifierOrder)
}

// chordKey is the canonical form for comparing chords: modifier order and duplicates don't
// matter.
func chordKey(chord platform.Chord) string {
	mods := slices.Clone(chord.Modifiers)
	slices.SortStableFunc(mods, func(a, b platform.KeyModifier) int {
		return cmp.Compare(rank(a), rank(b))
	})
	mods = slices.Compact(mods)
	var b strings.Builder
	for _, m := range mods {
		b.WriteString(string(m))
		b.WriteByte('+')
	}
	b.WriteByte(0)
	b.WriteString(strings.TrimSpace(chord.Key))
	return b.String()
}

func isASCIIGraphic(c rune) bool { return c >= 0x21 && c <= 0x7e }

func isASCIIAlphanumeric(c rune) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func looksLikeEmail(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || strings.Contains(domain, "@") {
		return false
	}
	for _, c := range s {
		if !isASCIIGraphic(c) || strings.ContainsRune(";(),<>", c) {
			return false
		}
	}
	labels := strings.Split(domain, ".")
	return len(labels) >= 2 && !slices.Contains(labels, "")
}

// Validate checks semantic constraints (ranges, chords distinct, ...).
func Validate(s *Settings) error {
	if s.Version > SettingsVersion {
		return &TooNewError{Found: s.Version}
	}

	if !s.League.AutoDetect && (s.League.League == nil || strings.TrimSpace(*s.League.League) == "") {
		return invalid("league.league", "must be set when league.auto_detect is false")
	}

	if len(s.Chat.Commands) > MaxMacros || len(s.Stash.Searches) > MaxMacros {
		return invalidf("chat.commands", "at most %d chat commands and stash searches", MaxMacros)
	}
	type text struct{ field, text string }
	var texts []text
	for _, c := range s.Chat.Commands {
		texts = append(texts, text{"chat.commands", c.Text})
	}
	for _, c := range s.Stash.Searches {
		texts = append(texts, text{"stash.searches", c.Text})
	}
	for _, t := range texts {
		if len(t.text) > 200 || strings.Contains(t.text, "\n") {
			return invalid(t.field, "text must be one line of at most 200 characters")
		}
	}
	if len(s.Chat.GameLog) > 1024 || strings.Contains(s.Chat.GameLog, "\n") {
		return invalid("chat.game_log", "the game log path must be one line of at most 1024 characters")
	}
	type hotkey struct {
		field string
		chord platform.Chord
	}
	hotkeys := []hotkey{
		{"hotkeys.price_check", s.Hotkeys.PriceCheck},
		{"hotkeys.price_check_pinned", s.Hotkeys.PriceCheckPinned},
		{"hotkeys.open_settings", s.Hotkeys.OpenSettings},
		{"hotkeys.open_trade", s.Hotkeys.OpenTrade},
	}
	for i, c := range s.Chat.Commands {
		hotkeys = append(hotkeys, hotkey{fmt.Sprintf("chat.commands[%d]", i), c.Chord})
	}
	for i, c := range s.Stash.Searches {
		hotkeys = append(hotkeys, hotkey{fmt.Sprintf("stash.searches[%d]", i), c.Chord})
	}
	for i, h := range hotkeys {
		if err := validateChord(h.field, h.chord); err != nil {
			return err
		}
		if h.chord.IsUnbound() {
			continue
		}
		k := chordKey(h.chord)
		for _, other := range hotkeys[:i] {
			if chordKey(other.chord) == k {
				return invalidf(h.field, "`%s` is already bound to %s", h.chord, other.field)
			}
		}
	}

	if len(s.Waystone.Flags) > 500 {
		return invalid("waystone.flags", "at most 500 flagged mods of 1-300 characters")
	}
	for k := range s.Waystone.Flags {
		if strings.TrimSpace(k) == "" || len(k) > 300 {
			return invalid("waystone.flags", "at most 500 flagged mods of 1-300 characters")
		}
	}
	if !(s.Overlay.Scale >= 0.75 && s.Overlay.Scale <= 1.5) {
		return invalidf("overlay.scale", "%s is not within 0.75..=1.5", formatFloat(s.Overlay.Scale))
	}
	if !(s.Overlay.BackdropDim >= 0 && s.Overlay.BackdropDim <= 0.9) {
		return invalidf("overlay.backdrop_dim", "%s is not within 0..=0.9", formatFloat(s.Overlay.BackdropDim))
	}
	if n := s.Trade.ListingCount; n < 10 || n > 100 || n%10 != 0 {
		return invalidf("trade.listing_count", "%d is not 10, 20, ... 100", n)
	}
	cols := s.Overlay.ListingColumns
	for i, col := range cols {
		if slices.Contains(cols[:i], col) {
			return invalid("overlay.listing_columns", "a column is listed twice")
		}
	}
	if !(s.Trade.WidenPct >= 0 && s.Trade.WidenPct <= 100) {
		return invalidf("trade.widen_pct", "%s is not within 0..=100", formatFloat(s.Trade.WidenPct))
	}
	if c := s.Trade.Contact; c != nil && !looksLikeEmail(*c) {
		return invalidf("trade.contact", "`%s` does not look like an e-mail address", *c)
	}

	if sid := s.Trade.POESESSID; sid != nil && *sid != "" {
		for _, c := range *sid {
			if !isASCIIAlphanumeric(c) {
				return invalid("trade.poesessid", "must contain only ASCII letters and digits")
			}
		}
	}

	if !(s.Overlay.Opacity >= 0.1 && s.Overlay.Opacity <= 1.0) {
		return invalidf("overlay.opacity", "%s is not within 0.1..=1.0", formatFloat(s.Overlay.Opacity))
	}
	if s.Overlay.AutoHideMS > 10_000 {
		return invalidf("overlay.auto_hide_ms", "%d exceeds 10000", s.Overlay.AutoHideMS)
	}
	return nil
}

// formatFloat formats f for an error message: the shortest decimal that round-trips,
// never an exponent.
func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
