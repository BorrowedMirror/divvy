package core

// Item text copied from a non-English game client, translated to the English text the
// parser and matcher understand. Trade searches use language-neutral stat ids and English
// names, so everything after this step works unchanged.
//
// The tables come from the game's own data (`cargo xtask data l10n`): keywords from its
// client strings, item class, base and unique names by row, and every wording of every stat
// from its stat descriptions, where each language lists its variants in the same order as
// English.

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Language is one game language's tables, as (English, localized) pairs. Each pair
// serialises as a two-element JSON array, like a Rust `(String, String)`.
type Language struct {
	// The game's name for the language, e.g. `German`.
	Name string `json:"name"`
	// Short code, e.g. `de`.
	Code string `json:"code"`
	// `Key: value` line keys (`Item Class`, `Rarity`, `Item Level`, `Armour`, ...).
	Keys [][2]string `json:"keys"`
	// Rarity values (`Rare`, `Unique`, ...).
	Rarities [][2]string `json:"rarities"`
	// Whole-line flags (`Corrupted`, `Unidentified`, `Desecrated Prefix`, ...).
	Flags [][2]string `json:"flags"`
	// Single words in a `Requires:` line (`Level`, `Str`, `Dex`, `Int`).
	Words [][2]string `json:"words"`
	// Patterns with `{0}`, replaced inside a line: mod headers (`Prefix Modifier "{0}"`,
	// ` (Tier: {0})`), `Superior {0}`, `Unidentified (Tier {0})`.
	Patterns [][2]string `json:"patterns"`
	Classes  [][2]string `json:"classes"`
	Bases    [][2]string `json:"bases"`
	Uniques  [][2]string `json:"uniques"`
	// Stat lines with indexed placeholders (`{0:+d} to maximum Life`); literal numbers are
	// kept as text.
	Stats [][2]string `json:"stats"`
}

// MarshalJSON writes serde-compatible JSON.
func (l Language) MarshalJSON() ([]byte, error) { return marshalStruct(l) }

// Translated is a text that was translated, and from which language.
type Translated struct {
	Text string
	// [Language.Name] of the client the text came from.
	Language string
}

// l10nSeparator: lines starting with this are section separators.
const l10nSeparator = "--------"

const l10nNum = `[+-]?` + reDigit + `+(?:[.,]` + reDigit + `+)?`

var (
	// l10nNumber is a number as it appears on an item: sign, decimals (`.` or `,`), and an
	// advanced-copy roll range right after it (`42(40-49)`).
	l10nNumber = regexp.MustCompile(l10nNum + `(?:\(` + l10nNum + `-` + l10nNum + `\))?`)
	// placeholder in a stat template: `{0}`, `{1:+d}`, `{0:d}`; a sign written just before it
	// belongs to the number.
	placeholder = regexp.MustCompile(`[+-]?\{(` + reDigit + `+)(?::([^}]*))?\}|[+-]?` + reDigit + `+(?:\.` + reDigit + `+)?`)
	// marker: lower-case markers the client appends to mod lines in every language: ` (implicit)`.
	marker = regexp.MustCompile(`(?: \([a-z]+\))+$`)
)

// slot is one numeric slot of a template, in order: a placeholder index (>= 0) or a
// literal number (-1).
type slot = int

// seg is a segment of an English stat template: text, or a placeholder index and whether
// the format asks for a sign (`+d`).
type seg struct {
	isValue bool
	text    string
	index   int
	signed  bool
}

type statPair struct {
	english []seg
	local   []slot
}

// templateShape: `{0:+d} zu maximalem Leben` -> (`# zu maximalem Leben`, slots).
func templateShape(template string) (string, []slot) {
	var slots []slot
	shape := replaceAllSubmatchFunc(placeholder, template, func(idx []int) string {
		s := -1
		if g, ok := group(template, idx, 1); ok {
			if n, ok := parseUsize(g); ok {
				s = n
			}
		}
		slots = append(slots, s)
		return "#"
	})
	return shape, slots
}

// lineShape: `+92 zu maximalem Leben` -> (`# zu maximalem Leben`, [`+92`]).
func lineShape(line string) (string, []string) {
	return l10nNumber.ReplaceAllLiteralString(line, "#"), l10nNumber.FindAllString(line, -1)
}

func englishSegments(template string) []seg {
	var out []seg
	last := 0
	for _, idx := range placeholder.FindAllStringSubmatchIndex(template, -1) {
		// A literal number stays text.
		g, ok := group(template, idx, 1)
		if !ok {
			continue
		}
		index, ok := parseUsize(g)
		if !ok {
			continue
		}
		out = append(out, seg{text: template[last:idx[0]]})
		f, hasFormat := group(template, idx, 2)
		signed := (hasFormat && strings.Contains(f, "+")) || strings.HasPrefix(template[idx[0]:idx[1]], "+")
		out = append(out, seg{isValue: true, index: index, signed: signed})
		last = idx[1]
	}
	return append(out, seg{text: template[last:]})
}

// englishNumber writes a number from the item the English way: `,` decimals become `.`,
// and a value the English template signs gets its `+`.
func englishNumber(token string, signed bool) string {
	t := token
	if i := strings.IndexByte(t, ','); i >= 0 && i+1 < len(t) && t[i+1] >= '0' && t[i+1] <= '9' {
		t = strings.ReplaceAll(t, ",", ".")
	}
	if signed && !strings.HasPrefix(t, "+") && !strings.HasPrefix(t, "-") {
		t = "+" + t
	}
	return t
}

type compiledPattern struct {
	re *regexp.Regexp
	en string
}

type compiled struct {
	name     string
	keys     map[string]string
	rarities map[string]string
	flags    map[string]string
	words    map[string]string
	patterns []compiledPattern
	classes  map[string]string
	bases    map[string]string
	// Localized base names, longest first, for magic items (`<prefix> <base> <suffix>`).
	basesLongest [][2]string
	uniques      map[string]string
	stats        map[string]statPair
	classKey     *string
	rarityKey    *string
}

func invert(pairs [][2]string) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		if _, ok := m[p[1]]; !ok {
			m[p[1]] = p[0]
		}
	}
	return m
}

// pattern: `Präfix-Modifikator "{0}"` -> a regex finding it in a line, and
// `Prefix Modifier "$1"`.
func pattern(en, local string) (compiledPattern, bool) {
	before, after, ok := strings.Cut(local, "{0}")
	if !ok {
		// A fixed phrase (`Implicit Modifier`).
		re, err := regexp.Compile(regexp.QuoteMeta(local))
		if err != nil {
			return compiledPattern{}, false
		}
		return compiledPattern{re, strings.ReplaceAll(en, "$", "$$")}, true
	}
	lazy := "(.+?)"
	if after == "" {
		lazy = "(.+)"
	}
	re, err := regexp.Compile(regexp.QuoteMeta(before) + lazy + regexp.QuoteMeta(after))
	if err != nil {
		return compiledPattern{}, false
	}
	return compiledPattern{re, strings.ReplaceAll(strings.ReplaceAll(en, "$", "$$"), "{0}", "${1}")}, true
}

func newCompiled(lang *Language) *compiled {
	stats := make(map[string]statPair, len(lang.Stats))
	for _, p := range lang.Stats {
		shape, slots := templateShape(p[1])
		if _, ok := stats[shape]; !ok {
			stats[shape] = statPair{english: englishSegments(p[0]), local: slots}
		}
	}
	basesLongest := make([][2]string, 0, len(lang.Bases))
	for _, p := range lang.Bases {
		basesLongest = append(basesLongest, [2]string{p[1], p[0]})
	}
	slices.SortStableFunc(basesLongest, func(a, b [2]string) int {
		return utf8.RuneCountInString(b[0]) - utf8.RuneCountInString(a[0])
	})
	key := func(en string) *string {
		for _, p := range lang.Keys {
			if p[0] == en {
				return new(p[1])
			}
		}
		return nil
	}
	var patterns []compiledPattern
	for _, p := range lang.Patterns {
		if cp, ok := pattern(p[0], p[1]); ok {
			patterns = append(patterns, cp)
		}
	}
	return &compiled{
		name:         lang.Name,
		keys:         invert(lang.Keys),
		rarities:     invert(lang.Rarities),
		flags:        invert(lang.Flags),
		words:        invert(lang.Words),
		patterns:     patterns,
		classes:      invert(lang.Classes),
		bases:        invert(lang.Bases),
		basesLongest: basesLongest,
		uniques:      invert(lang.Uniques),
		stats:        stats,
		classKey:     key("Item Class"),
		rarityKey:    key("Rarity"),
	}
}

func (c *compiled) applyPatterns(line string) string {
	out := line
	for _, p := range c.patterns {
		out = p.re.ReplaceAllString(out, p.en)
	}
	return out
}

func (c *compiled) base(name string) (string, bool) {
	if en, ok := c.bases[name]; ok {
		return en, true
	}
	// `Superior Base` is a pattern (`{0} (hochwertig)` in German).
	unwrapped := c.applyPatterns(name)
	if unwrapped != name {
		inner := strings.TrimPrefix(unwrapped, "Superior ")
		if en, ok := c.bases[inner]; ok {
			return "Superior " + en, true
		}
	}
	// A magic item's name wraps its base in affixes.
	for _, p := range c.basesLongest {
		if strings.Contains(name, p[0]) {
			return p[1], true
		}
	}
	return "", false
}

func (c *compiled) stat(line string) (string, bool) {
	shape, tokens := lineShape(line)
	pair, ok := c.stats[shape]
	if !ok || len(tokens) != len(pair.local) {
		return "", false
	}
	values := map[int]string{}
	for i, s := range pair.local {
		if s >= 0 {
			values[s] = tokens[i]
		}
	}
	var out strings.Builder
	for _, sg := range pair.english {
		if !sg.isValue {
			out.WriteString(sg.text)
			continue
		}
		v, ok := values[sg.index]
		if !ok {
			return "", false
		}
		out.WriteString(englishNumber(v, sg.signed))
	}
	return out.String(), true
}

// header turns `Key: value` into its English form, or false when the key is not one we
// know.
func (c *compiled) header(line string) (string, bool) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		if key, value, ok = strings.Cut(line, "："); !ok {
			return "", false
		}
	}
	en, ok := c.keys[trimSpace(key)]
	if !ok {
		return "", false
	}
	value = trimSpace(value)
	switch en {
	case "Item Class":
		if v, ok := c.classes[value]; ok {
			value = v
		}
	case "Rarity":
		if v, ok := c.rarities[value]; ok {
			value = v
		}
	case "Requires":
		parts := strings.Split(value, ",")
		for i, part := range parts {
			words := strings.Fields(part)
			for j, w := range words {
				if v, ok := c.words[w]; ok {
					words[j] = v
				}
			}
			parts[i] = strings.Join(words, " ")
		}
		value = strings.Join(parts, ", ")
	}
	if value == "" {
		return en + ":", true
	}
	return en + ": " + value, true
}

func (c *compiled) line(line string) string {
	if h, ok := c.header(line); ok {
		return h
	}
	if f, ok := c.flags[line]; ok {
		return f
	}
	if strings.HasPrefix(line, "{") {
		return c.applyPatterns(line)
	}
	body, mark := line, ""
	if m := marker.FindStringIndex(line); m != nil {
		body, mark = line[:m[0]], line[m[0]:m[1]]
	}
	// ` — Unscalable Value` and similar tails in advanced copies stay as they are.
	tail := ""
	if i := strings.Index(body, " — "); i >= 0 {
		body, tail = body[:i], body[i:]
	}
	if en, ok := c.stat(body); ok {
		return en + tail + mark
	}
	if patterned := c.applyPatterns(line); patterned != line {
		return patterned
	}
	return line
}

func (c *compiled) translate(text string) string {
	var out []string
	var rarity *string
	// Name lines: after the header, before the first separator.
	inNames := false
	var names []string
	flush := func() {
		n := len(names)
		for i, name := range names {
			var en string
			var ok bool
			if i+1 == n {
				en, ok = c.base(name)
			} else if rarity != nil && *rarity == "Unique" {
				en, ok = c.uniques[name]
			}
			if !ok {
				en = name
			}
			out = append(out, en)
		}
		names = names[:0]
	}
	for _, raw := range rustLines(text) {
		line := trimSpace(raw)
		if strings.HasPrefix(line, l10nSeparator) {
			if inNames {
				flush()
				inNames = false
			}
			out = append(out, line)
			continue
		}
		if inNames {
			if line != "" {
				names = append(names, line)
			}
			continue
		}
		translated := c.line(line)
		if r, ok := strings.CutPrefix(translated, "Rarity: "); ok {
			rarity = new(r)
			inNames = true
		}
		out = append(out, translated)
	}
	if inNames {
		flush()
	}
	return strings.Join(out, "\n")
}

// detects: the first line's key is this language's `Item Class` (or `Rarity`, for gems).
func (c *compiled) detects(firstKey string) bool {
	return (c.classKey != nil && *c.classKey == firstKey) || (c.rarityKey != nil && *c.rarityKey == firstKey)
}

// Translator translates item text from any of the languages it was built with. The zero
// value knows no languages.
type Translator struct {
	languages []*compiled
}

// NewTranslator compiles the given languages.
func NewTranslator(languages []Language) *Translator {
	t := &Translator{languages: make([]*compiled, 0, len(languages))}
	for i := range languages {
		t.languages = append(t.languages, newCompiled(&languages[i]))
	}
	return t
}

// Translate returns the text in English, when it came from one of the known non-English
// clients; nil for English (or unrecognised) text.
func (t *Translator) Translate(text string) *Translated {
	var first string
	found := false
	for _, l := range rustLines(text) {
		if l = trimSpace(l); l != "" {
			first, found = l, true
			break
		}
	}
	if !found {
		return nil
	}
	key, _, ok := strings.Cut(first, ":")
	if !ok {
		if key, _, ok = strings.Cut(first, "："); !ok {
			return nil
		}
	}
	key = trimSpace(key)
	if key == "Item Class" || key == "Rarity" {
		return nil
	}
	for _, lang := range t.languages {
		if lang.detects(key) {
			return &Translated{Text: lang.translate(text), Language: lang.name}
		}
	}
	return nil
}

// IsEmpty reports whether no languages are known.
func (t *Translator) IsEmpty() bool { return len(t.languages) == 0 }
