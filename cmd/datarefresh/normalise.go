package main

// Pure normalisation from upstream JSON into the internal/core catalog records.
//
// Every function here is deterministic: outputs are sorted and de-duplicated so that
// re-running the refresh on identical inputs produces byte-identical assets.

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/BorrowedMirror/divvy/internal/core"
)

// imageHost serves the relative `image` paths of `/api/trade2/data/static`.
const imageHost = "https://web.poecdn.com"

// The upstream JSON is decoded into the records below. Each has a check that the fields the
// normalisation needs are there, so an upstream format change fails the refresh instead of
// producing empty data. A field that may legitimately be empty is a pointer: nil means
// missing (or null).

// envelope decodes the `{"result": [group, ...]}` shape shared by the trade2 data endpoints,
// checking every group.
func envelope[G interface{ check() error }](raw []byte, what string) ([]G, error) {
	var env struct {
		Result []G `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", what, err)
	}
	if env.Result == nil {
		return nil, fmt.Errorf("parsing %s: missing field `result`", what)
	}
	for i, g := range env.Result {
		if err := g.check(); err != nil {
			return nil, fmt.Errorf("parsing %s: result[%d]: %w", what, i, err)
		}
	}
	return env.Result, nil
}

// required fails with the name of the first empty field among name/value pairs.
func required(fields ...string) error {
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i+1] == "" {
			return fmt.Errorf("missing field `%s`", fields[i])
		}
	}
	return nil
}

// checkEach checks every entry of a group.
func checkEach[E interface{ check() error }](entries []E) error {
	if entries == nil {
		return errors.New("missing field `entries`")
	}
	for i, e := range entries {
		if err := e.check(); err != nil {
			return fmt.Errorf("entries[%d]: %w", i, err)
		}
	}
	return nil
}

type statGroup struct {
	ID      string      `json:"id"`
	Entries []statEntry `json:"entries"`
}

func (g statGroup) check() error {
	if err := required("id", g.ID); err != nil {
		return err
	}
	return checkEach(g.Entries)
}

type statEntry struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func (e statEntry) check() error { return required("id", e.ID, "text", e.Text) }

type itemGroup struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Entries []itemEntry `json:"entries"`
}

func (g itemGroup) check() error {
	if err := required("id", g.ID, "label", g.Label); err != nil {
		return err
	}
	return checkEach(g.Entries)
}

type itemEntry struct {
	BaseType string  `json:"type"`
	Name     *string `json:"name"`
	Disc     *string `json:"disc"`
	Flags    struct {
		Unique bool `json:"unique"`
	} `json:"flags"`
}

func (e itemEntry) check() error { return required("type", e.BaseType) }

type staticGroup struct {
	ID      string        `json:"id"`
	Entries []staticEntry `json:"entries"`
}

func (g staticGroup) check() error {
	if err := required("id", g.ID); err != nil {
		return err
	}
	return checkEach(g.Entries)
}

type staticEntry struct {
	ID string `json:"id"`
	// Text is empty for separators.
	Text  *string `json:"text"`
	Image *string `json:"image"`
}

func (e staticEntry) check() error {
	if e.Text == nil {
		return errors.New("missing field `text`")
	}
	return required("id", e.ID)
}

// repoeBase is a RePoE `base_items.json` value (keyed by metadata path).
type repoeBase struct {
	// Name is empty for some internal bases.
	Name         *string `json:"name"`
	ItemClass    string  `json:"item_class"`
	ReleaseState string  `json:"release_state"`
}

// decodeRepoeBases decodes RePoE's `base_items.json`.
func decodeRepoeBases(raw []byte) (map[string]repoeBase, error) {
	var bases map[string]repoeBase
	if err := json.Unmarshal(raw, &bases); err != nil {
		return nil, err
	}
	for path, b := range bases {
		if b.Name == nil {
			return nil, fmt.Errorf("%s: missing field `name`", path)
		}
		if err := required("item_class", b.ItemClass); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return bases, nil
}

// repoeItemClass is a RePoE `item_classes.json` value (keyed by item class id).
type repoeItemClass struct {
	Name string `json:"name"`
}

// statAliases returns alternative wordings for a trade stat template.
//
//   - sign variants: `# to X` also matches `+# to X` and (negated) `-# to X`; `+# to X`
//     also matches `# to X` and (negated) `-# to X`.
//   - `#% increased` <-> `#% reduced` and `#% more` <-> `#% less` (negated).
//
// Values of a negated alias must be multiplied by -1 (the matcher reads unsigned numbers
// from the templatized line).
func statAliases(text string) []core.StatAlias {
	var out []core.StatAlias
	push := func(t string, negate bool) {
		if !slices.ContainsFunc(out, func(a core.StatAlias) bool { return a.Text == t }) {
			out = append(out, core.StatAlias{Text: t, Negate: negate})
		}
	}
	if rest, ok := strings.CutPrefix(text, "+#"); ok {
		push("#"+rest, false)
		push("-#"+rest, true)
	} else if strings.HasPrefix(text, "#") {
		push("+"+text, false)
		push("-"+text, true)
	}
	for _, p := range [][2]string{
		{"% increased ", "% reduced "}, {"% reduced ", "% increased "}, {"% more ", "% less "}, {"% less ", "% more "},
	} {
		if strings.Contains(text, p[0]) {
			push(strings.Replace(text, p[0], p[1], 1), true)
			break
		}
	}
	return slices.DeleteFunc(out, func(a core.StatAlias) bool { return a.Text == text })
}

// wordingVariants returns exact alternative wordings of a trade template as it appears on
// items (never negated):
//
//   - a trailing disambiguation suffix is dropped: `#% increased Armour (Local)` ->
//     `#% increased Armour` (items never print these suffixes; the matcher disambiguates
//     local/global by item);
//   - map-device wording: `... in Map` -> `... in your Maps` (tablets print the latter);
//   - literal numbers baked into the template are templatized the way the matcher does
//     (`... per 10 Dexterity` -> `... per # Dexterity`);
//   - level-less skill grants: `Grants Skill: Level # Parry` -> `Grants Skill: Parry`.
func wordingVariants(text string) []string {
	out := []string{text}
	push := func(t string) {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	if stripped, ok := stripSuffix(text); ok {
		push(stripped)
	}
	for i := range out {
		if maps, ok := inYourMaps(out[i]); ok {
			push(maps)
		}
	}
	for i := range out {
		rest, isSkill := strings.CutPrefix(out[i], "Grants Skill: Level # ")
		push(core.Templatize(out[i]))
		if isSkill {
			push("Grants Skill: " + rest)
		}
	}
	return out[1:]
}

// stripSuffix turns `X (Local)` into `X`, for a trailing parenthesised capitalised label.
func stripSuffix(text string) (string, bool) {
	body, ok := strings.CutSuffix(text, ")")
	if !ok {
		return "", false
	}
	open := strings.LastIndex(body, " (")
	if open < 0 {
		return "", false
	}
	label := body[open+2:]
	if label == "" || label[0] < 'A' || label[0] > 'Z' {
		return "", false
	}
	for _, c := range label {
		if c != ' ' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return "", false
		}
	}
	return text[:open], true
}

// inYourMaps replaces the whole word `in Map` with `in your Maps`.
func inYourMaps(text string) (string, bool) {
	const word = " in Map"
	for at := 0; ; {
		i := strings.Index(text[at:], word)
		if i < 0 {
			return "", false
		}
		i += at
		after := text[i+len(word):]
		if after == "" || after[0] == ' ' {
			return text[:i] + " in your Maps" + after, true
		}
		at = i + len(word)
	}
}

// normaliseStats turns `/api/trade2/data/stats` into stats sorted by (kind, id), with
// aliases.
//
// kind is the trade2 group id (`explicit`, `implicit`, `rune`, ...). trade2 lists some ids
// several times with different wordings; the first is Text, the others become non-negated
// aliases, as do [wordingVariants]. Generated [statAliases] that collide with a real
// template of the same kind are dropped so they cannot create false ambiguities; real
// wordings and wording variants are always kept.
func normaliseStats(raw []byte) ([]core.StatDef, error) {
	groups, err := envelope[statGroup](raw, "trade2 stats")
	if err != nil {
		return nil, err
	}
	type key struct{ kind, id string }
	texts := map[key][]string{}
	for _, g := range groups {
		for _, e := range g.Entries {
			k := key{g.ID, e.ID}
			if !slices.Contains(texts[k], e.Text) {
				texts[k] = append(texts[k], e.Text)
			}
		}
	}
	keys := make([]key, 0, len(texts))
	isStat := map[[2]string]bool{}
	for k, ts := range texts {
		keys = append(keys, k)
		for _, t := range ts {
			isStat[[2]string{k.kind, t}] = true
		}
	}
	slices.SortFunc(keys, func(a, b key) int { return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.id, b.id)) })
	out := make([]core.StatDef, 0, len(keys))
	for _, k := range keys {
		ts := texts[k]
		primary := ts[0]
		var wordings []string
		for _, t := range ts {
			if t != primary {
				wordings = append(wordings, t)
			}
			wordings = append(wordings, wordingVariants(t)...)
		}
		aliases := []core.StatAlias{}
		push := func(a core.StatAlias) {
			if a.Text != primary && !slices.ContainsFunc(aliases, func(b core.StatAlias) bool { return b.Text == a.Text }) {
				aliases = append(aliases, a)
			}
		}
		for _, w := range wordings {
			push(core.StatAlias{Text: w})
		}
		for _, w := range append([]string{primary}, wordings...) {
			for _, a := range statAliases(w) {
				if !isStat[[2]string{k.kind, a.Text}] {
					push(a)
				}
			}
		}
		out = append(out, core.StatDef{ID: k.id, Text: primary, Kind: k.kind, Aliases: aliases})
	}
	return out, nil
}

// tradeCategories maps a RePoE item class id to its trade category id
// (`/api/trade2/data/filters` `type_filters.category`).
var tradeCategories = map[string]string{
	"Helmet":            "armour.helmet",
	"Body Armour":       "armour.chest",
	"Gloves":            "armour.gloves",
	"Boots":             "armour.boots",
	"Shield":            "armour.shield",
	"Buckler":           "armour.buckler",
	"Focus":             "armour.focus",
	"Quiver":            "armour.quiver",
	"Ring":              "accessory.ring",
	"Amulet":            "accessory.amulet",
	"Belt":              "accessory.belt",
	"Jewel":             "jewel",
	"LifeFlask":         "flask.life",
	"ManaFlask":         "flask.mana",
	"UtilityFlask":      "flask.charm",
	"Claw":              "weapon.claw",
	"Dagger":            "weapon.dagger",
	"One Hand Sword":    "weapon.onesword",
	"One Hand Axe":      "weapon.oneaxe",
	"One Hand Mace":     "weapon.onemace",
	"Spear":             "weapon.spear",
	"Flail":             "weapon.flail",
	"Two Hand Sword":    "weapon.twosword",
	"Two Hand Axe":      "weapon.twoaxe",
	"Two Hand Mace":     "weapon.twomace",
	"Warstaff":          "weapon.warstaff",
	"Talisman":          "weapon.talisman",
	"Bow":               "weapon.bow",
	"Crossbow":          "weapon.crossbow",
	"Wand":              "weapon.wand",
	"Sceptre":           "weapon.sceptre",
	"Staff":             "weapon.staff",
	"Map":               "map.waystone",
	"Breachstone":       "map.breachstone",
	"TowerAugmentation": "map.tablet",
	"UltimatumKey":      "map.ultimatum",
	"Relic":             "sanctum.relic",
	"Omen":              "currency.omen",
	"SoulCore":          "currency.soulcore",
	"Active Skill Gem":  "gem.activegem",
	"Support Skill Gem": "gem.supportgem",
	"Meta Skill Gem":    "gem.metagem",
}

// normaliseBases turns the non-unique `/api/trade2/data/items` entries, enriched with
// RePoE item classes, into bases sorted by name.
//
// Category comes from the RePoE item class when known, else the trade2 group id;
// ItemClass is the in-game `Item Class:` header (RePoE item class display name), else the
// trade2 group label.
func normaliseBases(itemsRaw []byte, repoeBases map[string]repoeBase, repoeClasses map[string]repoeItemClass) ([]core.BaseDef, error) {
	groups, err := envelope[itemGroup](itemsRaw, "trade2 items")
	if err != nil {
		return nil, err
	}
	// name -> item class id, preferring released bases with a known trade category and
	// then the lexicographically smallest metadata path.
	type rank struct {
		unreleased, unknown bool
		path, class         string
	}
	less := func(a, b rank) bool {
		if a.unreleased != b.unreleased {
			return !a.unreleased
		}
		if a.unknown != b.unknown {
			return !a.unknown
		}
		return a.path < b.path
	}
	classOf := map[string]rank{}
	for path, b := range repoeBases {
		_, known := tradeCategories[b.ItemClass]
		r := rank{b.ReleaseState != "released", !known, path, b.ItemClass}
		if cur, ok := classOf[*b.Name]; !ok || less(r, cur) {
			classOf[*b.Name] = r
		}
	}
	byName := map[string]core.BaseDef{}
	for _, g := range groups {
		for _, e := range g.Entries {
			if e.Name != nil || e.Flags.Unique {
				continue
			}
			if _, ok := byName[e.BaseType]; ok {
				continue
			}
			category, itemClass := g.ID, g.Label
			if r, ok := classOf[e.BaseType]; ok {
				if c, ok := tradeCategories[r.class]; ok {
					category = c
				}
				if c, ok := repoeClasses[r.class]; ok && c.Name != "" {
					itemClass = c.Name
				}
			}
			byName[e.BaseType] = core.BaseDef{Name: e.BaseType, Category: category, ItemClass: itemClass}
		}
	}
	out := make([]core.BaseDef, 0, len(byName))
	for _, b := range byName {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b core.BaseDef) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// normaliseUniques turns the unique `/api/trade2/data/items` entries into uniques sorted
// by (name, base type). Legacy variants (entries with a `disc`) are skipped.
func normaliseUniques(itemsRaw []byte) ([]core.UniqueDef, error) {
	groups, err := envelope[itemGroup](itemsRaw, "trade2 items")
	if err != nil {
		return nil, err
	}
	set := map[core.UniqueDef]bool{}
	for _, g := range groups {
		for _, e := range g.Entries {
			if e.Flags.Unique && e.Disc == nil && e.Name != nil {
				set[core.UniqueDef{Name: *e.Name, BaseType: e.BaseType}] = true
			}
		}
	}
	out := make([]core.UniqueDef, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b core.UniqueDef) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.BaseType, b.BaseType))
	})
	return out, nil
}

// normaliseStatics turns `/api/trade2/data/static` into statics sorted by (group, id).
// Separator entries (`id == "sep"`) are skipped; relative image paths are made absolute.
func normaliseStatics(raw []byte) ([]core.StaticDef, error) {
	groups, err := envelope[staticGroup](raw, "trade2 static")
	if err != nil {
		return nil, err
	}
	type key struct{ group, id string }
	byKey := map[key]core.StaticDef{}
	for _, g := range groups {
		for _, e := range g.Entries {
			k := key{g.ID, e.ID}
			if e.ID == "sep" || *e.Text == "" {
				continue
			}
			if _, ok := byKey[k]; ok {
				continue
			}
			image := e.Image
			if image != nil && strings.HasPrefix(*image, "/") {
				abs := imageHost + *image
				image = &abs
			}
			byKey[k] = core.StaticDef{ID: e.ID, Text: *e.Text, Group: g.ID, Image: image}
		}
	}
	keys := make([]key, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b key) int { return cmp.Or(cmp.Compare(a.group, b.group), cmp.Compare(a.id, b.id)) })
	out := make([]core.StaticDef, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	return out, nil
}

// gameVersion reads the game version from the RePoE index page title
// (`RePoE - PoE2 version 0.5.5.5`).
func gameVersion(indexHTML string) *string {
	_, rest, ok := strings.Cut(indexHTML, "PoE2 version ")
	if !ok {
		return nil
	}
	end := 0
	for end < len(rest) && (rest[end] == '.' || ('0' <= rest[end] && rest[end] <= '9')) {
		end++
	}
	v := strings.TrimRight(rest[:end], ".")
	if v == "" {
		return nil
	}
	return &v
}
