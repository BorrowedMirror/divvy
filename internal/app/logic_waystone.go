package app

import (
	"cmp"
	"encoding/json"
	"errors"
	"iter"
	"slices"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// WaystoneModGroup is what a waystone mod affects, for grouping the catalogue. Sent to the
// frontend. Groups order as declared (player, monster, area), not alphabetically.
type WaystoneModGroup string

// WaystoneModGroup variants (serde snake_case).
const (
	WaystonePlayer  WaystoneModGroup = "player"
	WaystoneMonster WaystoneModGroup = "monster"
	WaystoneArea    WaystoneModGroup = "area"
)

// WaystoneModGroupValues lists every [WaystoneModGroup] in declaration (sort) order.
func WaystoneModGroupValues() []WaystoneModGroup {
	return []WaystoneModGroup{WaystonePlayer, WaystoneMonster, WaystoneArea}
}

// UnmarshalJSON accepts only the serde variant names.
func (g *WaystoneModGroup) UnmarshalJSON(b []byte) error {
	return unmarshalLogicEnum(b, g, "WaystoneModGroup", WaystoneModGroupValues())
}

// Compare orders groups as the Rust enum derived Ord did (declaration order); unknown values
// sort last.
func (g WaystoneModGroup) Compare(o WaystoneModGroup) int {
	rank := func(v WaystoneModGroup) int {
		if i := slices.Index(WaystoneModGroupValues(), v); i >= 0 {
			return i
		}
		return len(WaystoneModGroupValues())
	}
	return cmp.Compare(rank(g), rank(o))
}

// WaystoneMod is one entry of the waystone mod catalogue (Text is the mod template, `#` for
// numbers). Sent to the frontend.
type WaystoneMod struct {
	Text  string           `json:"text"`
	Group WaystoneModGroup `json:"group"`
}

// MarshalJSON writes serde-compatible JSON.
func (m WaystoneMod) MarshalJSON() ([]byte, error) {
	type plain WaystoneMod
	return core.MarshalSerde(plain(m))
}

// UnmarshalJSON requires both fields, as serde does.
func (m *WaystoneMod) UnmarshalJSON(b []byte) error {
	var v struct {
		Text  *string           `json:"text"`
		Group *WaystoneModGroup `json:"group"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch {
	case v.Text == nil:
		return errors.New("WaystoneMod: missing field `text`")
	case v.Group == nil:
		return errors.New("WaystoneMod: missing field `group`")
	}
	*m = WaystoneMod{Text: *v.Text, Group: *v.Group}
	return nil
}

// ExtraWaystoneMods are waystone mods the trade site's stat list does not carry (seen on real
// waystones).
var ExtraWaystoneMods = []string{"Players have -#% to all maximum Resistances", "Monsters have #% increased Life"}

// WaystoneGroup is the group of a waystone mod by its wording; ok is false when it does not
// read like one.
func WaystoneGroup(text string) (group WaystoneModGroup, ok bool) {
	switch {
	case strings.HasPrefix(text, "Players "):
		return WaystonePlayer, true
	case slices.ContainsFunc([]string{"Monsters ", "Rare Monsters", "Magic Monsters", "Map Boss", "Delirium Monsters"},
		func(p string) bool { return strings.HasPrefix(text, p) }):
		return WaystoneMonster, true
	case strings.HasPrefix(text, "Area "), strings.HasPrefix(text, "Map "),
		strings.Contains(text, "in this Area"), strings.Contains(text, "Pack Size"):
		return WaystoneArea, true
	}
	return "", false
}

// WaystoneCatalog is the catalogue: explicit trade stats that read like waystone mods, plus
// [ExtraWaystoneMods], de-duplicated and sorted by group, then text.
func WaystoneCatalog(explicitStatTexts iter.Seq[string]) []WaystoneMod {
	var out []WaystoneMod
	add := func(t string) {
		if strings.Contains(t, "\n") {
			return
		}
		if g, ok := WaystoneGroup(t); ok {
			out = append(out, WaystoneMod{Text: t, Group: g})
		}
	}
	for t := range explicitStatTexts {
		add(t)
	}
	for _, t := range ExtraWaystoneMods {
		add(t)
	}
	slices.SortStableFunc(out, func(a, b WaystoneMod) int {
		return cmp.Or(a.Group.Compare(b.Group), strings.Compare(a.Text, b.Text))
	})
	return slices.CompactFunc(out, func(a, b WaystoneMod) bool { return a.Text == b.Text })
}
