// Matches corpus items against the real bundled trade2 catalog from internal/data. It is an
// external test package because internal/data imports core.
package core_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core/coretest"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
)

type idText struct{ id, text string }

// corpusTexts returns every corpus item's id and raw text.
func corpusTexts(t *testing.T) []idText {
	t.Helper()
	items, err := coretest.LoadCorpus()
	require.NoError(t, err)
	out := make([]idText, len(items))
	for i, it := range items {
		out[i] = idText{it.Entry.ID, it.Text}
	}
	return out
}

type idItem struct {
	id   string
	item *core.ParsedItem
}

func corpus(t *testing.T) []idItem {
	t.Helper()
	var out []idItem
	for _, it := range corpusTexts(t) {
		item, err := core.ParseItem(it.text)
		require.NoError(t, err)
		out = append(out, idItem{it.id, item})
	}
	return out
}

func bundled(t *testing.T) *data.GameData {
	t.Helper()
	d, err := data.Bundled()
	require.NoError(t, err, "bundled data")
	return d
}

func matched(t *testing.T, id string) *core.ParsedItem {
	t.Helper()
	d := bundled(t)
	for _, c := range corpus(t) {
		if c.id == id {
			core.MatchStats(c.item, d)
			return c.item
		}
	}
	t.Fatalf("missing %s", id)
	return nil
}

func statOf(item *core.ParsedItem, text string) *string {
	for _, m := range item.Modifiers {
		if m.Text == text {
			return m.StatID
		}
	}
	return nil
}

func TestLocalArmourOnGloves(t *testing.T) {
	gloves := matched(t, "gloves/rare-gloves-advanced")
	assert.Equal(t, new("explicit.stat_3484657501"), statOf(gloves, "+98 to Armour"))
}

func TestLocalEvasionOnHelmet(t *testing.T) {
	helmet := matched(t, "helmet/rare-hunter-hood-advanced")
	assert.Equal(t, new("explicit.stat_53045048"), statOf(helmet, "+109 to Evasion Rating"))
}

func TestBowAdditionalArrowMatchesPluralStat(t *testing.T) {
	bow := matched(t, "weapon/rare-bow-advanced")
	i := slices.IndexFunc(bow.Modifiers, func(m core.Modifier) bool { return m.Text == "Bow Attacks fire an additional Arrow" })
	require.GreaterOrEqual(t, i, 0)
	assert.NotNil(t, bow.Modifiers[i].StatID, "additional arrow unmatched")
	assert.Equal(t, []float64{1}, bow.Modifiers[i].Values)
}

func TestCorpusMatchRateFloor(t *testing.T) {
	d := bundled(t)
	total, hits := 0, 0
	var misses []string
	for _, c := range corpus(t) {
		core.MatchStats(c.item, d)
		for _, m := range c.item.Modifiers {
			if m.Kind != core.ModExplicit && m.Kind != core.ModImplicit {
				continue
			}
			total++
			if m.StatID != nil {
				hits++
			} else {
				misses = append(misses, c.id+": "+m.Text)
			}
		}
	}
	rate := float64(hits) / float64(total)
	t.Logf("match rate %d/%d = %.1f%%\nunmatched:\n%s", hits, total, rate*100, strings.Join(misses, "\n"))
	assert.GreaterOrEqual(t, rate, 0.85, "match rate %.1f%% below 85%%", rate*100)
}

func stdOpts(status core.OnlineStatus) *core.QueryOptions {
	return &core.QueryOptions{League: "Standard", Status: status, WidenPct: 10, EnableAllStats: false}
}

// Real user report: a sceptre search returned nothing. Spirit must use the sceptre's local
// stat, the prefix and desecrated Spirit mods must be one summed filter, desecrated mods are
// searched as explicit, and "reduced" requirements get an upper bound.
func TestSceptreSpiritSearchMatchesTradeListings(t *testing.T) {
	sceptre := matched(t, "weapon/rare-sceptre-spirit-advanced")
	assert.Equal(t, new("explicit.stat_3984865854"), statOf(sceptre, "55% increased Spirit"))
	assert.Equal(t, new("desecrated.stat_3984865854"), statOf(sceptre, "13% increased Spirit"))
	f := core.BuildFilters(sceptre, stdOpts(core.StatusOnline))
	var spirit []core.StatFilter
	for _, s := range f.Stats {
		if strings.HasSuffix(s.StatID, "stat_3984865854") {
			spirit = append(spirit, s)
		}
	}
	require.Len(t, spirit, 1, "one summed Spirit filter: %+v", f.Stats)
	assert.Equal(t, "explicit.stat_3984865854", spirit[0].StatID)
	assert.Equal(t, []float64{68}, spirit[0].ItemValues)
	assert.InDelta(t, 61.0, *spirit[0].Min, 0)
	assert.Equal(t, "68% increased Spirit", spirit[0].Text)
	for _, s := range f.Stats {
		assert.False(t, strings.HasPrefix(s.StatID, "desecrated."), "%+v", f.Stats)
	}
	i := slices.IndexFunc(f.Stats, func(s core.StatFilter) bool { return s.Text == "30% reduced Attribute Requirements" })
	require.GreaterOrEqual(t, i, 0)
	assert.Nil(t, f.Stats[i].Min)
	assert.InDelta(t, -27.0, *f.Stats[i].Max, 0)
}

func TestGlobalSpiritOnAmuletsStaysGlobal(t *testing.T) {
	d := bundled(t)
	var ids []string
	for _, s := range d.StatsByTemplate("#% increased Spirit", core.ModExplicit) {
		ids = append(ids, s.Stat.ID)
	}
	assert.True(t, slices.Contains(ids, "explicit.stat_1416406066") && slices.Contains(ids, "explicit.stat_3984865854"), "%v", ids)
	text := "Item Class: Amulets\nRarity: Rare\nGale Charm\nGold Amulet\n--------\nItem Level: 80\n--------\n20% increased Spirit\n"
	amulet, err := core.ParseItem(text)
	require.NoError(t, err)
	core.MatchStats(amulet, d)
	assert.Equal(t, new("explicit.stat_1416406066"), statOf(amulet, "20% increased Spirit"))
}

func TestStatFiltersCarryModTiersFromAdvancedCopies(t *testing.T) {
	sceptre := matched(t, "weapon/rare-sceptre-spirit-advanced")
	f := core.BuildFilters(sceptre, stdOpts(core.StatusAny))
	tier := func(text string) []int {
		for _, s := range f.Stats {
			if s.Text == text {
				return s.Tiers
			}
		}
		return nil
	}
	assert.Equal(t, []int{2}, tier("30% reduced Attribute Requirements"))
	assert.Equal(t, []int{9}, tier("Allies in your Presence Regenerate 2.2 Life per second"))
	assert.Equal(t, []int{3, 7}, tier("68% increased Spirit"), "summed prefix + desecrated row")
}

// The waystone mod check keys flags by template: the templates the parser produces for real
// waystones must be exactly the strings flagged by default.
func TestWaystoneTemplatesMatchTheDefaultDangerFlags(t *testing.T) {
	var templates []string
	for _, id := range []string{"waystone/corrupted-tier-16-advanced", "waystone/rare-tier-15"} {
		for _, m := range matched(t, id).Modifiers {
			templates = append(templates, m.Template)
		}
	}
	for _, danger := range []string{
		"Players have -#% to all maximum Resistances",
		"Players have #% less Recovery Rate of Life and Energy Shield",
		"Monsters fire # additional Projectiles",
	} {
		assert.Contains(t, templates, danger)
	}
}

// Every corpus item written as each bundled client language would show it (its header and
// every stat line the tables cover, by running them backwards) translates back to text that
// matches exactly the same trade stats as the English copy.
func TestLocalizedCorpusMatchesLikeEnglish(t *testing.T) {
	d := bundled(t)
	texts := corpusTexts(t)
	// `\p{Nd}` is a Unicode digit (Go's `\d` is ASCII-only).
	const n = `[+-]?\p{Nd}+(?:\.\p{Nd}+)?`
	number := regexp.MustCompile(n + `(?:\(` + n + `-` + n + `\))?`)
	slot := regexp.MustCompile(`[+-]?\{(\p{Nd}+)(?::[^}]*)?\}|` + n)
	marker := regexp.MustCompile(`(?: \([a-z]+\))+$`)
	statIDs := func(text string) []*string {
		item, err := core.ParseItem(text)
		require.NoError(t, err)
		core.MatchStats(item, d)
		var out []*string
		for _, m := range item.Modifiers {
			out = append(out, m.StatID)
		}
		return out
	}
	languages := d.Bundle().Languages
	require.NotEmpty(t, languages)
	for _, lang := range languages {
		lookup := func(pairs [][2]string, en string) (string, bool) {
			for _, p := range pairs {
				if p[0] == en {
					return p[1], true
				}
			}
			return "", false
		}
		byShape := map[string][2]string{}
		for _, p := range lang.Stats {
			shape := slot.ReplaceAllLiteralString(p[0], "#")
			if _, ok := byShape[shape]; !ok {
				byShape[shape] = p
			}
		}
		localizedLines := 0
		for _, it := range texts {
			var out []string
			inNames := false
			for _, line := range rustLines(it.text) {
				line = strings.TrimSpace(line)
				if inNames {
					if strings.HasPrefix(line, "--------") {
						inNames = false
					} else {
						name, ok := lookup(lang.Bases, line)
						if !ok {
							if name, ok = lookup(lang.Uniques, line); !ok {
								name = line
							}
						}
						out = append(out, name)
						continue
					}
				}
				if v, ok := strings.CutPrefix(line, "Item Class: "); ok {
					class, ok := lookup(lang.Classes, v)
					if !ok {
						class = v
					}
					key, _ := lookup(lang.Keys, "Item Class")
					out = append(out, key+": "+class)
					continue
				}
				if v, ok := strings.CutPrefix(line, "Rarity: "); ok {
					rarity, ok := lookup(lang.Rarities, v)
					if !ok {
						rarity = v
					}
					key, _ := lookup(lang.Keys, "Rarity")
					out = append(out, key+": "+rarity)
					inNames = true
					continue
				}
				body, tail := line, ""
				if m := marker.FindStringIndex(line); m != nil {
					body, tail = line[:m[0]], line[m[0]:]
				}
				tokens := number.FindAllString(body, -1)
				if p, ok := byShape[number.ReplaceAllLiteralString(body, "#")]; ok {
					var slots []int
					for _, c := range slot.FindAllStringSubmatch(p[0], -1) {
						s := -1
						if c[1] != "" {
							if v, err := parseIndex(c[1]); err == nil {
								s = v
							}
						}
						slots = append(slots, s)
					}
					if len(slots) == len(tokens) {
						values := map[int]string{}
						for i, s := range slots {
							if s >= 0 {
								values[s] = tokens[i]
							}
						}
						written := slot.ReplaceAllStringFunc(p[1], func(m string) string {
							c := slot.FindStringSubmatch(m)
							if c[1] != "" {
								if v, err := parseIndex(c[1]); err == nil {
									if val, ok := values[v]; ok {
										return val
									}
								}
							}
							return m
						})
						out = append(out, written+tail)
						localizedLines++
						continue
					}
				}
				out = append(out, line)
			}
			translated, ok := d.Translate(strings.Join(out, "\n"))
			require.True(t, ok, "%s: %s not detected", lang.Name, it.id)
			// A magic item's name wraps its base; compare the base the catalog resolves.
			base := func(item *core.ParsedItem) string {
				if b := d.Base(item.BaseType); b != nil {
					return b.Name
				}
				if b := d.BaseInMagicName(item.BaseType); b != nil {
					return b.Name
				}
				return item.BaseType
			}
			back, err := core.ParseItem(translated.Text)
			require.NoError(t, err)
			original, err := core.ParseItem(it.text)
			require.NoError(t, err)
			assert.Equal(t, base(original), base(back), "%s %s", lang.Name, it.id)
			assert.Equal(t, original.Name, back.Name, "%s %s", lang.Name, it.id)
			assert.Equal(t, statIDs(it.text), statIDs(translated.Text), "%s %s:\n%s\n--- translated back:\n%s",
				lang.Name, it.id, strings.Join(out, "\n"), translated.Text)
		}
		t.Logf("%s: %d stat lines localized and matched", lang.Name, localizedLines)
		assert.GreaterOrEqual(t, localizedLines, 150, "%s: only %d corpus stat lines localized", lang.Name, localizedLines)
	}
}

// rustLines is `str::lines`.
func rustLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

func parseIndex(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
