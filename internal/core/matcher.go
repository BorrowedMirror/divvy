package core

// Maps parsed modifiers to trade stat ids.
//
// Matching works on templates: every number in a modifier line is replaced by `#`
// ([Templatize]) and looked up through [Catalog.StatsByTemplate]. When several stats
// share a template the matcher prefers the `(Local)` variant on weapons and armour and
// the global one elsewhere. Crafted / fractured / desecrated / sanctified lines fall back
// to the explicit stat when the catalog has no dedicated kind.

import (
	"regexp"
	"slices"
	"strings"
)

// numberRe matches numbers, optionally with thousands separators and decimals (`1,043`,
// `4.40`), with Unicode digits.
var numberRe = regexp.MustCompile(reDigit + `{1,3}(?:,` + reDigit + `{3})+(?:\.` + reDigit + `+)?|` + reDigit + `+(?:\.` + reDigit + `+)?`)

// Templatize replaces every number in line with `#` (keeping `+`/`-` signs as text),
// matching trade templates.
func Templatize(line string) string {
	return numberRe.ReplaceAllLiteralString(strings.TrimSpace(line), "#")
}

// extractValues returns the numeric values of line in order of appearance (unsigned; signs
// stay in the template).
func extractValues(line string) []float64 {
	out := []float64{}
	for _, m := range numberRe.FindAllString(line, -1) {
		if v, ok := parseFloat(strings.ReplaceAll(m, ",", "")); ok {
			out = append(out, v)
		}
	}
	return out
}

// MatchStats fills StatID on every modifier of item that catalog can match (nil when it
// cannot).
//
// It also resolves the base type of magic items (whose single header line carries the
// affix names): when the catalog finds a base inside the line, the full line moves to Name
// and BaseType becomes the bare base. Negated matches (e.g. a "reduced" line matched to an
// "increased" stat) flip the sign of the values.
func MatchStats(item *ParsedItem, catalog Catalog) {
	if item.Rarity == RarityMagic && item.Name == nil {
		if base := catalog.BaseInMagicName(item.BaseType); base != nil && base.Name != item.BaseType {
			item.Name = new(item.BaseType)
			item.BaseType = base.Name
		}
	}
	m := matcher{catalog: catalog, local: prefersLocal(item.Class)}
	for i := range item.Modifiers {
		mod := &item.Modifiers[i]
		match, ok := m.matchOne(mod.Template, mod.Kind)
		if !ok {
			if plural, index, found := pluraliseAdditional(mod.Template); found {
				if match, ok = m.matchOne(plural, mod.Kind); ok {
					// "an additional Arrow" carries an implicit value of 1.
					mod.Values = slices.Insert(mod.Values, min(index, len(mod.Values)), 1.0)
				}
			}
		}
		if !ok {
			mod.StatID = nil
			continue
		}
		mod.StatID = new(match.Stat.ID)
		if match.Negate {
			for j := range mod.Values {
				mod.Values[j] = -mod.Values[j]
			}
		}
	}
}

// matcher looks templates up in a catalog for one item.
type matcher struct {
	catalog Catalog
	// local: the item prefers the `(Local)` variants of stats (weapons and armour).
	local bool
}

// matchOne finds the stat for template, if any.
func (m *matcher) matchOne(template string, kind ModKind) (StatMatch, bool) {
	candidates := m.candidates(template, kind)
	if len(candidates) == 0 {
		if rest, ok := strings.CutPrefix(template, "-"); ok {
			// `-#% to Fire Resistance` is listed as `+#% to Fire Resistance` with negative values.
			candidates = m.candidates("+"+rest, kind)
			for i := range candidates {
				candidates[i].Negate = !candidates[i].Negate
			}
		}
	}
	switch len(candidates) {
	case 0:
		return StatMatch{}, false
	case 1:
		return candidates[0], true
	}
	return breakTie(candidates, template, m.local), true
}

// candidates for template; on weapons/armour the `(Local)` variants win when present.
func (m *matcher) candidates(template string, kind ModKind) []StatMatch {
	if m.local {
		if locals := m.lookup(template+" (Local)", kind); len(locals) > 0 {
			return locals
		}
	}
	return m.lookup(template, kind)
}

func (m *matcher) lookup(template string, kind ModKind) []StatMatch {
	found := slices.Clone(m.catalog.StatsByTemplate(template, kind))
	if len(found) > 0 {
		return found
	}
	switch kind {
	case ModCrafted, ModFractured, ModDesecrated, ModSanctified:
		return slices.Clone(m.catalog.StatsByTemplate(template, ModExplicit))
	default:
		return nil
	}
}

// breakTie makes a deterministic choice among several stats sharing a template: exact
// text first, then the local/global variant fitting the item, then the lowest id.
func breakTie(candidates []StatMatch, template string, local bool) StatMatch {
	localText := template + " (Local)"
	narrow := func(keep func(*StatDef) bool) {
		if slices.ContainsFunc(candidates, func(c StatMatch) bool { return keep(c.Stat) }) {
			candidates = slices.DeleteFunc(candidates, func(c StatMatch) bool { return !keep(c.Stat) })
		}
	}
	narrow(func(s *StatDef) bool { return s.Text == template || s.Text == localText })
	narrow(func(s *StatDef) bool { return isLocal(s) == local })
	slices.SortStableFunc(candidates, func(a, b StatMatch) int { return strings.Compare(a.Stat.ID, b.Stat.ID) })
	return candidates[0]
}

// singularRe is `\b(?:an|a) (?:additional )?([A-Z][A-Za-z]*)\b` without the word
// boundaries, which are Unicode-aware and so checked by hand in [findSingular] (Go's `\b`
// is ASCII-only).
var singularRe = regexp.MustCompile(`^(?:an|a) (?:additional )?([A-Z][A-Za-z]*)`)

// singularMatch is a match of [singularRe] in a template: the whole phrase and the noun,
// as byte offsets.
type singularMatch struct {
	start, end         int
	nounStart, nounEnd int
}

// findSingular returns the leftmost match of singularRe between word boundaries. After the
// greedy ASCII noun no shorter match can satisfy the trailing boundary (the next character
// would be an ASCII letter), so one anchored attempt per start suffices.
func findSingular(s string) (singularMatch, bool) {
	for i := range len(s) {
		if s[i] != 'a' || wordBefore(s, i) {
			continue
		}
		m := singularRe.FindStringSubmatchIndex(s[i:])
		if m == nil || wordAt(s, i+m[1]) {
			continue
		}
		return singularMatch{i + m[0], i + m[1], i + m[2], i + m[3]}, true
	}
	return singularMatch{}, false
}

// pluraliseAdditional turns `an additional Arrow` / `a Projectile` into
// `# additional Arrows`; returns the new template and the index of the inserted `#` among
// the template's placeholders.
func pluraliseAdditional(template string) (string, int, bool) {
	m, ok := findSingular(template)
	if !ok {
		return "", 0, false
	}
	noun := template[m.nounStart:m.nounEnd]
	index := strings.Count(template[:m.start], "#")
	plural := template[:m.start] + "# additional " + noun + "s" + template[m.end:]
	return plural, index, true
}

// unmarkedLocalStats are local stats the trade API does not mark with "(Local)": they
// share their text with a global stat, so text alone cannot tell them apart. Matched by
// the stat hash after the group prefix (`explicit.`, `desecrated.`, `fractured.`, `rune.`
// ...). Verified against live trade2 searches on 2026-11-06, e.g. `#% increased Spirit`:
// `stat_3984865854` is on 10000+ sceptres, `stat_1416406066` on none.
var unmarkedLocalStats = []string{"stat_3984865854"}

func isLocal(stat *StatDef) bool {
	if strings.HasSuffix(stat.Text, "(Local)") {
		return true
	}
	_, hash, ok := strings.Cut(stat.ID, ".")
	return ok && slices.Contains(unmarkedLocalStats, hash)
}

// prefersLocal: weapons and armour pieces use local variants of damage / defence stats.
func prefersLocal(class ItemClass) bool {
	switch class.Kind {
	case ClassWeapon, ClassBodyArmour, ClassHelmet, ClassGloves, ClassBoots, ClassShield, ClassBuckler, ClassFocus:
		return true
	default:
		return false
	}
}
