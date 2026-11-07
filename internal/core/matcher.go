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
// `4.40`). `\d` is Unicode-aware as in Rust's regex.
var numberRe = regexp.MustCompile(reDigit + `{1,3}(?:,` + reDigit + `{3})+(?:\.` + reDigit + `+)?|` + reDigit + `+(?:\.` + reDigit + `+)?`)

// Templatize replaces every number in line with `#` (keeping `+`/`-` signs as text),
// matching trade templates.
func Templatize(line string) string {
	return numberRe.ReplaceAllLiteralString(trimSpace(line), "#")
}

// ExtractValues returns the numeric values of line in order of appearance (unsigned; signs
// stay in the template).
func ExtractValues(line string) []float64 {
	out := []float64{}
	for _, m := range numberRe.FindAllString(line, -1) {
		if v, ok := parseF64(strings.ReplaceAll(m, ",", "")); ok {
			out = append(out, v)
		}
	}
	return out
}

// MatchOutcomeKind is the variant of a [MatchOutcome].
type MatchOutcomeKind int

// MatchOutcomeKind values.
const (
	Unmatched MatchOutcomeKind = iota
	Matched
	Ambiguous
)

// MatchOutcome is the outcome per modifier.
type MatchOutcome struct {
	Kind MatchOutcomeKind
	// StatID and Negated are set for [Matched].
	StatID  string
	Negated bool
	// Candidates is set for [Ambiguous].
	Candidates []string
}

// StatMatcher fills stat ids from a [Catalog].
type StatMatcher struct {
	catalog Catalog
}

// NewStatMatcher returns a matcher over catalog.
func NewStatMatcher(catalog Catalog) *StatMatcher { return &StatMatcher{catalog: catalog} }

// Catalog returns the matcher's catalog.
func (m *StatMatcher) Catalog() Catalog { return m.catalog }

// MatchItem fills StatID on every modifier that can be matched; returns per-modifier
// outcomes.
//
// Also resolves the base type of magic items (whose single header line carries the affix
// names): when the catalog finds a base inside the line, the full line moves to Name and
// BaseType becomes the bare base. Negated matches (e.g. a "reduced" line matched to an
// "increased" stat) flip the sign of the values.
func (m *StatMatcher) MatchItem(item *ParsedItem) []MatchOutcome {
	if item.Rarity == RarityMagic && item.Name == nil {
		if base := m.catalog.BaseInMagicName(item.BaseType); base != nil && base.Name != item.BaseType {
			item.Name = ptr(item.BaseType)
			item.BaseType = base.Name
		}
	}
	local := prefersLocal(item.Class)
	out := make([]MatchOutcome, 0, len(item.Modifiers))
	for i := range item.Modifiers {
		mod := &item.Modifiers[i]
		outcome := m.matchOne(mod.Template, mod.Kind, local)
		if outcome.Kind == Unmatched {
			if plural, index, ok := pluraliseAdditional(mod.Template); ok {
				outcome = m.matchOne(plural, mod.Kind, local)
				if outcome.Kind != Unmatched {
					// "an additional Arrow" carries an implicit value of 1.
					mod.Values = slices.Insert(mod.Values, min(index, len(mod.Values)), 1.0)
				}
			}
		}
		switch outcome.Kind {
		case Matched:
			mod.StatID = ptr(outcome.StatID)
			if outcome.Negated {
				for j := range mod.Values {
					mod.Values[j] = -mod.Values[j]
				}
			}
		default:
			mod.StatID = nil
		}
		out = append(out, outcome)
	}
	return out
}

func (m *StatMatcher) matchOne(template string, kind ModKind, local bool) MatchOutcome {
	candidates := m.candidates(template, kind, local)
	if len(candidates) == 0 {
		if rest, ok := strings.CutPrefix(template, "-"); ok {
			// `-#% to Fire Resistance` is listed as `+#% to Fire Resistance` with negative values.
			candidates = m.candidates("+"+rest, kind, local)
			for i := range candidates {
				candidates[i].Negate = !candidates[i].Negate
			}
		}
	}
	switch len(candidates) {
	case 0:
		return MatchOutcome{Kind: Unmatched}
	case 1:
		return MatchOutcome{Kind: Matched, StatID: candidates[0].Stat.ID, Negated: candidates[0].Negate}
	}
	c := breakTie(candidates, template, local)
	return MatchOutcome{Kind: Matched, StatID: c.Stat.ID, Negated: c.Negate}
}

// candidates for template; on weapons/armour the `(Local)` variants win when present.
func (m *StatMatcher) candidates(template string, kind ModKind, local bool) []StatMatch {
	if local {
		if locals := m.lookup(template+" (Local)", kind); len(locals) > 0 {
			return locals
		}
	}
	return m.lookup(template, kind)
}

func (m *StatMatcher) lookup(template string, kind ModKind) []StatMatch {
	found := slices.Clone(m.catalog.StatsByTemplate(template, kind))
	if len(found) == 0 {
		switch kind {
		case ModCrafted, ModFractured, ModDesecrated, ModSanctified:
			return slices.Clone(m.catalog.StatsByTemplate(template, ModExplicit))
		}
	}
	return found
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
// boundaries: Go's `\b` is ASCII-only while Rust's is Unicode, so the boundaries are checked
// by hand in [pluraliseAdditional].
var singularRe = regexp.MustCompile(`^(?:an|a) (?:additional )?([A-Z][A-Za-z]*)`)

// findSingular returns the leftmost match of the Rust pattern (start, end, noun start, noun
// end). After the greedy ASCII noun no shorter match can satisfy the trailing `\b` (the
// next character would be an ASCII letter), so one anchored attempt per start suffices.
func findSingular(s string) []int {
	for i := 0; i < len(s); i++ {
		if (s[i] != 'a') || wordBefore(s, i) {
			continue
		}
		m := singularRe.FindStringSubmatchIndex(s[i:])
		if m == nil || wordAt(s, i+m[1]) {
			continue
		}
		return []int{i + m[0], i + m[1], i + m[2], i + m[3]}
	}
	return nil
}

// pluraliseAdditional turns `an additional Arrow` / `a Projectile` into
// `# additional Arrows`; returns the new template and the index of the inserted `#` among
// the template's placeholders.
func pluraliseAdditional(template string) (string, int, bool) {
	m := findSingular(template)
	if m == nil {
		return "", 0, false
	}
	start, end := m[0], m[1]
	noun := template[m[2]:m[3]]
	index := strings.Count(template[:start], "#")
	plural := template[:start] + "# additional " + noun + "s" + template[end:]
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
	}
	return false
}
