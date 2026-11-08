package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// fakeCatalog is a minimal in-memory catalog for tests.
type fakeCatalog struct {
	stats   []StatDef
	bases   []BaseDef
	uniques []UniqueDef
	statics []StaticDef
}

func kindPrefix(kind ModKind) string { return string(kind) }

func (c *fakeCatalog) StatsByTemplate(template string, kind ModKind) []StatMatch {
	var out []StatMatch
	for i := range c.stats {
		s := &c.stats[i]
		if s.Kind != kindPrefix(kind) {
			continue
		}
		if s.Text == template {
			out = append(out, StatMatch{Stat: s})
			continue
		}
		for _, a := range s.Aliases {
			if a.Text == template {
				out = append(out, StatMatch{Stat: s, Negate: a.Negate})
				break
			}
		}
	}
	return out
}

func (c *fakeCatalog) Base(name string) *BaseDef {
	for i := range c.bases {
		if c.bases[i].Name == name {
			return &c.bases[i]
		}
	}
	return nil
}

// BaseInMagicName mirrors `max_by_key(len)`, which returns the last maximum.
func (c *fakeCatalog) BaseInMagicName(fullName string) *BaseDef {
	var best *BaseDef
	for i := range c.bases {
		b := &c.bases[i]
		if strings.Contains(fullName, b.Name) && (best == nil || len(b.Name) >= len(best.Name)) {
			best = b
		}
	}
	return best
}

func (c *fakeCatalog) Unique(name string) *UniqueDef {
	for i := range c.uniques {
		if c.uniques[i].Name == name {
			return &c.uniques[i]
		}
	}
	return nil
}

func (c *fakeCatalog) StaticItem(text string) *StaticDef {
	for i := range c.statics {
		if c.statics[i].Text == text {
			return &c.statics[i]
		}
	}
	return nil
}

var _ Catalog = (*fakeCatalog)(nil)

func stat(id, text, kind string) StatDef {
	return StatDef{ID: id, Text: text, Kind: kind, Aliases: []StatAlias{}}
}

func aliased(id, text, alias string) StatDef {
	s := stat(id, text, "explicit")
	s.Aliases = append(s.Aliases, StatAlias{Text: alias, Negate: false})
	return s
}

func newFakeCatalog() *fakeCatalog {
	increasedMS := stat("explicit.stat_2250533757", "#% increased Movement Speed", "explicit")
	increasedMS.Aliases = append(increasedMS.Aliases, StatAlias{Text: "#% reduced Movement Speed", Negate: true})
	return &fakeCatalog{
		stats: []StatDef{
			stat("explicit.stat_3299347043", "+# to maximum Life", "explicit"),
			stat("explicit.stat_3372524247", "+#% to Fire Resistance", "explicit"),
			stat("explicit.stat_4220027924", "+#% to Cold Resistance", "explicit"),
			stat("implicit.stat_3372524247", "+#% to Fire Resistance", "implicit"),
			stat("rune.stat_3372524247", "+#% to Fire Resistance", "rune"),
			stat("explicit.stat_1940865751", "Adds # to # Physical Damage (Local)", "explicit"),
			stat("explicit.stat_3032590688", "Adds # to # Physical Damage to Attacks", "explicit"),
			stat("explicit.stat_2223678961", "Adds # to # Physical Damage", "explicit"),
			stat("explicit.stat_dup_a", "#% increased Rarity of Items found", "explicit"),
			stat("explicit.stat_dup_b", "#% increased Rarity of Items found", "explicit"),
			increasedMS,
			stat("explicit.stat_arrows", "Bow Attacks fire # additional Arrows", "explicit"),
			aliased("explicit.stat_attr_a", "# to all Attributes", "+# to all Attributes"),
			stat("explicit.stat_attr_b", "+# to all Attributes", "explicit"),
			aliased("explicit.stat_spirit_a", "#% increased Spirit (Local)", "#% increased Spirit"),
			aliased("explicit.stat_spirit_b", "#% increased Spirit", "#% increased Spirit"),
		},
		bases: []BaseDef{
			{Name: "Hunter Hood", Category: "armour.helmet", ItemClass: "Helmets"},
			{Name: "Expert Hunter Hood", Category: "armour.helmet", ItemClass: "Helmets"},
		},
		uniques: []UniqueDef{{Name: "Goldrim", BaseType: "Felt Cap"}},
		statics: []StaticDef{{ID: "divine", Text: "Divine Orb", Group: "Currency", Image: nil}},
	}
}

func TestTemplatizeExamples(t *testing.T) {
	assert.Equal(t, "+# to maximum Life", Templatize("+42 to maximum Life"))
	assert.Equal(t, "Adds # to # Physical Damage", Templatize("Adds 10 to 1,043 Physical Damage"))
	assert.Equal(t, "-#% to Cold Resistance", Templatize("-4.5% to Cold Resistance"))
	assert.Equal(t, "Grants Skill: Level # Raise Zombie", Templatize("  Grants Skill: Level 12 Raise Zombie "))
	assert.Equal(t, "no numbers", Templatize("no numbers"))
}

func TestValuesExamples(t *testing.T) {
	assert.Equal(t, []float64{10, 1043}, extractValues("Adds 10 to 1,043 Physical Damage"))
	assert.Equal(t, []float64{4.4}, extractValues("+4.40% to x"))
	assert.Empty(t, extractValues("none"))
}

type modLine struct {
	text string
	kind ModKind
}

func itemWith(t *testing.T, class ItemClass, rarity Rarity, base string, lines []modLine) *ParsedItem {
	t.Helper()
	item, err := ParseItem("Item Class: x\nRarity: Rare\n" + base)
	require.NoError(t, err, "parses")
	item.Class = class
	item.Rarity = rarity
	item.BaseType = base
	item.Name = nil
	item.Modifiers = []Modifier{}
	for _, l := range lines {
		item.Modifiers = append(item.Modifiers, Modifier{
			Text:     l.text,
			Template: Templatize(l.text),
			Kind:     l.kind,
			Values:   extractValues(l.text),
		})
	}
	return item
}

func statIDs(item *ParsedItem) []*string {
	out := make([]*string, 0, len(item.Modifiers))
	for _, m := range item.Modifiers {
		out = append(out, m.StatID)
	}
	return out
}

func TestMatchesKindsAliasesAndLocals(t *testing.T) {
	cat := newFakeCatalog()
	item := itemWith(t, WeaponClass("Bows"), RarityRare, "Recurve Bow", []modLine{
		{"+42 to maximum Life", ModExplicit},
		{"+20% to Fire Resistance", ModImplicit},
		{"+10% to Fire Resistance", ModRune},
		{"+12% to Fire Resistance", ModFractured},
		{"-10% to Cold Resistance", ModExplicit},
		{"10% reduced Movement Speed", ModExplicit},
		{"Adds 3 to 7 Physical Damage", ModExplicit},
		{"20% increased Rarity of Items found", ModExplicit},
		{"Totally unknown", ModExplicit},
		{"Bow Attacks fire an additional Arrow", ModExplicit},
		{"+10 to all Attributes", ModExplicit},
		{"Has a Strange thing", ModExplicit},
	})
	MatchStats(item, cat)
	assert.Equal(t, []*string{
		new("explicit.stat_3299347043"),
		new("implicit.stat_3372524247"),
		new("rune.stat_3372524247"),
		new("explicit.stat_3372524247"),
		new("explicit.stat_4220027924"),
		new("explicit.stat_2250533757"),
		new("explicit.stat_1940865751"),
		new("explicit.stat_dup_a"),
		nil,
		new("explicit.stat_arrows"),
		new("explicit.stat_attr_b"),
		nil,
	}, statIDs(item))
	// `-10% to Cold Resistance` matched the `+#%` stat negated.
	assert.Equal(t, []float64{-10}, item.Modifiers[4].Values)
	assert.Equal(t, []float64{-10}, item.Modifiers[5].Values)
	assert.Equal(t, []float64{1}, item.Modifiers[9].Values)

	// On jewellery the global variant wins.
	ring := itemWith(t, ClassRing.Class(), RarityRare, "Ruby Ring", []modLine{
		{"Adds 3 to 7 Physical Damage", ModExplicit},
		{"30% increased Spirit", ModExplicit},
	})
	MatchStats(ring, cat)
	assert.Equal(t, new("explicit.stat_2223678961"), ring.Modifiers[0].StatID)
	assert.Equal(t, new("explicit.stat_spirit_b"), ring.Modifiers[1].StatID)
}

func TestResolvesMagicBase(t *testing.T) {
	cat := newFakeCatalog()
	item := itemWith(t, ClassHelmet.Class(), RarityMagic, "Hale Expert Hunter Hood of the Kiln", nil)
	MatchStats(item, cat)
	assert.Equal(t, "Expert Hunter Hood", item.BaseType)
	assert.Equal(t, new("Hale Expert Hunter Hood of the Kiln"), item.Name)
	// Already a bare base: untouched.
	bare := itemWith(t, ClassHelmet.Class(), RarityMagic, "Hunter Hood", nil)
	MatchStats(bare, cat)
	assert.Nil(t, bare.Name)
	// Exercise the remaining fake-catalog lookups.
	assert.NotNil(t, cat.Base("Hunter Hood"))
	assert.NotNil(t, cat.Unique("Goldrim"))
	assert.NotNil(t, cat.StaticItem("Divine Orb"))
}

func TestPluraliseExamples(t *testing.T) {
	p, i, ok := pluraliseAdditional("Bow Attacks fire an additional Arrow")
	assert.True(t, ok)
	assert.Equal(t, "Bow Attacks fire # additional Arrows", p)
	assert.Equal(t, 0, i)
	p, i, ok = pluraliseAdditional("#% chance to fire a Projectile")
	assert.True(t, ok)
	assert.Equal(t, "#% chance to fire # additional Projectiles", p)
	assert.Equal(t, 1, i)
	_, _, ok = pluraliseAdditional("+# to maximum Life")
	assert.False(t, ok)
}

// The hand-rolled Unicode word boundaries of the singular pattern.
func TestPluraliseUnicodeWordBoundaries(t *testing.T) {
	_, _, ok := pluraliseAdditional("éa Projectile")
	assert.False(t, ok, "a non-ASCII letter before `a` is a word character")
	_, _, ok = pluraliseAdditional("fire a Projectileé")
	assert.False(t, ok, "a non-ASCII letter after the noun is a word character")
	p, _, ok := pluraliseAdditional("éa x, a Projectile!")
	assert.True(t, ok)
	assert.Equal(t, "éa x, # additional Projectiles!", p)
}

func TestTemplatizeIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringOfN(anyRune(), 0, 80, -1).Draw(t, "s")
		s = strings.ReplaceAll(s, "\n", "")
		once := Templatize(s)
		if Templatize(once) != once {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, once, Templatize(once))
		}
		if strings.ContainsAny(once, "0123456789") {
			t.Fatalf("digits left in %q", once)
		}
	})
}
