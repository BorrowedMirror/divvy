package app

import (
	"cmp"
	"iter"
	"slices"
	"strings"
)

// WaystoneModGroup is what a waystone mod affects, for grouping the catalogue. Sent to the
// frontend. Groups order as declared (player, monster, area), not alphabetically.
type WaystoneModGroup string

// WaystoneModGroup values.
const (
	WaystonePlayer  WaystoneModGroup = "player"
	WaystoneMonster WaystoneModGroup = "monster"
	WaystoneArea    WaystoneModGroup = "area"
)

// waystoneModGroups lists every [WaystoneModGroup] in sort order.
var waystoneModGroups = []WaystoneModGroup{WaystonePlayer, WaystoneMonster, WaystoneArea}

// UnmarshalText accepts only the [WaystoneModGroup] values.
func (g *WaystoneModGroup) UnmarshalText(b []byte) error {
	return parseEnum(g, b, waystoneModGroups...)
}

// rank is the group's position in the sort order; unknown values sort last.
func (g WaystoneModGroup) rank() int {
	if i := slices.Index(waystoneModGroups, g); i >= 0 {
		return i
	}
	return len(waystoneModGroups)
}

// WaystoneMod is one entry of the waystone mod catalogue (Text is the mod template, `#` for
// numbers). Sent to the frontend.
type WaystoneMod struct {
	Text  string           `json:"text"`
	Group WaystoneModGroup `json:"group"`
}

// compareWaystoneMods orders mods by group, then text.
func compareWaystoneMods(a, b WaystoneMod) int {
	return cmp.Or(cmp.Compare(a.Group.rank(), b.Group.rank()), strings.Compare(a.Text, b.Text))
}

// extraWaystoneMods are waystone mods the trade site's stat list does not carry (seen on real
// waystones).
var extraWaystoneMods = []string{"Players have -#% to all maximum Resistances", "Monsters have #% increased Life"}

// monsterPrefixes start the monster mods.
var monsterPrefixes = []string{"Monsters ", "Rare Monsters", "Magic Monsters", "Map Boss", "Delirium Monsters"}

// waystoneGroup is the group of a waystone mod by its wording; ok is false when it does not
// read like one.
func waystoneGroup(text string) (group WaystoneModGroup, ok bool) {
	switch {
	case strings.HasPrefix(text, "Players "):
		return WaystonePlayer, true
	case slices.ContainsFunc(monsterPrefixes, func(p string) bool { return strings.HasPrefix(text, p) }):
		return WaystoneMonster, true
	case strings.HasPrefix(text, "Area "), strings.HasPrefix(text, "Map "),
		strings.Contains(text, "in this Area"), strings.Contains(text, "Pack Size"):
		return WaystoneArea, true
	}
	return "", false
}

// waystoneCatalog is the catalogue: explicit trade stats that read like waystone mods, plus
// [extraWaystoneMods], de-duplicated and sorted by group, then text.
func waystoneCatalog(explicitStatTexts iter.Seq[string]) []WaystoneMod {
	var out []WaystoneMod
	add := func(t string) {
		if strings.Contains(t, "\n") {
			return
		}
		if g, ok := waystoneGroup(t); ok {
			out = append(out, WaystoneMod{Text: t, Group: g})
		}
	}
	for t := range explicitStatTexts {
		add(t)
	}
	for _, t := range extraWaystoneMods {
		add(t)
	}
	slices.SortStableFunc(out, compareWaystoneMods)
	return slices.CompactFunc(out, func(a, b WaystoneMod) bool { return a.Text == b.Text })
}
