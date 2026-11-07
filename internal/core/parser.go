package core

// Parser for PoE2 clipboard item text (Ctrl+C and Ctrl+Alt+C formats).
//
// Sections are separated by lines of `--------`. The first section is the header
// (`Item Class:`, `Rarity:`, name lines). Meta gems omit `Item Class:` and start at
// `Rarity: Gem`.
//
// Every later section is classified by its first line:
//   - flag sections (`Corrupted`, `Unidentified`, `Mirrored`, `Sanctified`, ...);
//   - `Note:`, `Requires:` / `Requirements:`, `Sockets:`, `Item Level:`;
//   - the first unclassified section after the header is the properties block
//     (`Name: value` lines plus free-text lines such as flask recovery);
//   - remaining free-text sections are modifiers. Each line may end with a marker
//     (`(implicit)`, `(rune)`, `(added rune)`, `(enchant)`, `(crafted)`, `(fractured)`,
//     `(desecrated)`, `(sanctified)`); unmarked lines are explicit. For uniques only the
//     first unmarked section holds explicits; later ones are flavour text. Usage-hint
//     sections ("Right click to ...", "Place into ...") are skipped, as are modifier
//     sections of currency and gems.
//
// Advanced copies (Ctrl+Alt+C) add `{ Prefix Modifier "Hale" (Tier: 7) — Life }` headers
// before the lines they describe and print rolls as `+42(40-49)`. Ranges are stripped from
// Text and recorded in [AdvancedModInfo.Ranges].

import (
	"math"
	"regexp"
	"slices"
	"strings"
)

// Separator is the section separator used in clipboard text.
const Separator = "--------"

// num is `-?\d+(?:\.\d+)?` with Rust's Unicode `\d`.
const num = `-?` + reDigit + `+(?:\.` + reDigit + `+)?`

var (
	rollRange      = regexp.MustCompile(`(` + num + `)\((` + num + `)-(` + num + `)\)`)
	advancedHeader = regexp.MustCompile(`^\{` + reSpace + `*(?P<kind>[^"(—}]*?)` + reSpace + `*(?:"(?P<name>[^"]*)")?` +
		reSpace + `*(?:\((?:Tier|Rank):` + reSpace + `*(?P<tier>` + reDigit + `+)\))?` + reSpace +
		`*(?:—` + reSpace + `*(?P<tags>[^}]*?))?` + reSpace + `*\}$`)
	parenSuffix = regexp.MustCompile(reSpace + `*\(([a-z ]+)\)`)
	baseTier    = regexp.MustCompile(`\((?:Tier|Level) (` + reDigit + `+)\)` + reSpace + `*$`)

	advKind = advancedHeader.SubexpIndex("kind")
	advName = advancedHeader.SubexpIndex("name")
	advTier = advancedHeader.SubexpIndex("tier")
)

// hintPrefixes are lines that start a usage-hint section rather than modifiers.
var hintPrefixes = []string{
	"Right click",
	"Right-click",
	"Place into",
	"Place this item",
	"Place an item",
	"Can be used",
	"Shift click",
	"Used automatically",
	"Travel to",
	"Use on",
	"Combine",
	"This is a Support Gem",
	"Can only hold charges",
	"Socket this into",
	"Take this item",
	"Bring this item",
	"Present this item",
}

// ParseItem parses clipboard text into a [ParsedItem]. Accepts `\r\n` or `\n`. The error
// is always a [ParseError].
func ParseItem(text string) (*ParsedItem, error) {
	if trimSpace(text) == "" {
		return nil, ParseError{Kind: ParseErrEmpty}
	}
	sections := splitSections(text)
	if len(sections) == 0 {
		return nil, ParseError{Kind: ParseErrEmpty}
	}
	item, err := parseHeader(sections[0], text)
	if err != nil {
		return nil, err
	}
	var state parserState
	for _, section := range sections[1:] {
		parseSection(item, &state, section)
	}
	finish(item)
	return item, nil
}

// LooksLikeItem is the cheap check used by the clipboard poller to decide whether text
// looks like an item.
func LooksLikeItem(text string) bool {
	t := strings.TrimLeftFunc(text, isSpaceRune)
	return strings.HasPrefix(t, "Item Class:") || strings.HasPrefix(t, "Rarity: Gem")
}

// splitSections returns sections as trimmed, non-empty lines.
func splitSections(text string) [][]string {
	out := [][]string{nil}
	for _, line := range rustLines(text) {
		line = trimSpace(line)
		if len(line) >= len(Separator) && strings.Trim(line, "-") == "" {
			out = append(out, nil)
		} else if line != "" {
			out[len(out)-1] = append(out[len(out)-1], line)
		}
	}
	return slices.DeleteFunc(out, func(s []string) bool { return len(s) == 0 })
}

func stripKey(line, key string) (string, bool) {
	rest, ok := strings.CutPrefix(line, key)
	if !ok {
		return "", false
	}
	rest, ok = strings.CutPrefix(rest, ":")
	if !ok {
		return "", false
	}
	return trimSpace(rest), true
}

func parseHeader(lines []string, raw string) (*ParsedItem, error) {
	i := 0
	var class *ItemClass
	if v, ok := stripKey(lines[0], "Item Class"); ok {
		i++
		class = new(ItemClassFromHeader(v))
	}
	if i >= len(lines) {
		return nil, ParseError{Kind: ParseErrNotAnItem}
	}
	rarityLine := lines[i]
	i++
	rarityValue, ok := stripKey(rarityLine, "Rarity")
	if !ok {
		return nil, ParseError{Kind: ParseErrNotAnItem}
	}
	rarity, ok := RarityFromHeader(rarityValue)
	if !ok {
		return nil, ParseError{Kind: ParseErrUnknownRarity, Value: rarityValue}
	}
	if class == nil {
		if rarity != RarityGem {
			return nil, ParseError{Kind: ParseErrNotAnItem}
		}
		// Meta gems are the only items copied without an `Item Class:` line.
		class = new(OtherClass("Meta Gems"))
	}
	names := lines[i:]
	var name *string
	var baseType string
	switch {
	case len(names) == 0:
		return nil, ParseError{Kind: ParseErrMalformed, Section: "header", Reason: "missing item name"}
	case (rarity == RarityRare || rarity == RarityUnique) && len(names) >= 2:
		name, baseType = new(names[0]), names[1]
	default:
		baseType = names[0]
	}
	if rarity == RarityNormal {
		baseType = strings.TrimPrefix(baseType, "Superior ")
	}
	c := refineClass(*class, baseType)
	item := &ParsedItem{
		Class:      c,
		Category:   c.Category(rarity),
		Rarity:     rarity,
		Name:       name,
		BaseType:   baseType,
		Properties: []Property{},
		Modifiers:  []Modifier{},
		Raw:        raw,
	}
	if m := baseTier.FindStringSubmatch(item.BaseType); m != nil {
		if n, ok := parseU32(m[1]); ok {
			switch item.Class.Kind {
			case ClassWaystone:
				item.WaystoneTier = new(n)
			case ClassUncutGem:
				item.GemLevel = new(n)
			}
		}
	}
	return item, nil
}

// refineClass: `Augment` and `Stackable Currency` cover several kinds; refine by base name.
func refineClass(class ItemClass, base string) ItemClass {
	switch class.Kind {
	case ClassRune:
		if strings.Contains(base, "Soul Core") || strings.HasPrefix(base, "Soul of ") {
			return ClassSoulCore.Class()
		}
	case ClassStackableCurrency:
		switch {
		case strings.Contains(base, "Essence of"):
			return ClassEssence.Class()
		case strings.HasPrefix(base, "Distilled "):
			return ClassDistilledEmotion.Class()
		case strings.HasSuffix(base, "Splinter"):
			return ClassSplinter.Class()
		case strings.HasSuffix(base, "Catalyst"):
			return ClassCatalyst.Class()
		case strings.HasPrefix(base, "Omen of"):
			return ClassOmen.Class()
		}
	}
	return class
}

type advancedState struct {
	info AdvancedModInfo
	kind ModKind
}

type parserState struct {
	propertiesDone bool
	explicitsDone  bool
	advanced       *advancedState
}

var flags = []string{
	"Corrupted",
	"Twice Corrupted",
	"Unidentified",
	"Mirrored",
	"Sanctified",
	"Fractured Item",
	"Split",
	"Unmodifiable",
}

func isFlag(line string) bool {
	return slices.Contains(flags, line) || strings.HasPrefix(line, "Unidentified (")
}

func parseSection(item *ParsedItem, state *parserState, lines []string) {
	first := lines[0]
	if !slices.ContainsFunc(lines, func(l string) bool { return !isFlag(l) }) {
		for _, l := range lines {
			switch {
			case l == "Corrupted" || l == "Twice Corrupted":
				item.Corrupted = true
			case l == "Mirrored":
				item.Mirrored = true
			case l == "Sanctified":
				item.Sanctified = true
			case strings.HasPrefix(l, "Unidentified"):
				item.Unidentified = true
			}
		}
		return
	}
	if note, ok := stripKey(first, "Note"); ok {
		item.Note = new(note)
		return
	}
	if strings.HasPrefix(first, "Requires") || strings.HasPrefix(first, "Requirements") {
		for _, l := range lines {
			parseRequirementLine(&item.Requirements, l)
		}
		state.propertiesDone = true
		return
	}
	for _, k := range []string{"Sockets", "Item Level", "Stack Size", "Area Level"} {
		if _, ok := stripKey(first, k); ok {
			for _, l := range lines {
				parsePropertyLine(item, l)
			}
			state.propertiesDone = true
			return
		}
	}
	if !state.propertiesDone {
		state.propertiesDone = true
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "{") }) {
			for _, l := range lines {
				parsePropertyLine(item, l)
			}
			return
		}
	}
	parseModSection(item, state, lines)
}

func parseRequirementLine(req *Requirements, line string) {
	clean := stripParenSuffixes(line)
	set := func(key string, n uint32) {
		switch asciiLower(key) {
		case "level":
			req.Level = new(n)
		case "str", "strength":
			req.Str = new(n)
		case "dex", "dexterity":
			req.Dex = new(n)
		case "int", "intelligence":
			req.Int = new(n)
		}
	}
	body, ok := strings.CutPrefix(clean, "Requires:")
	if !ok {
		body, ok = strings.CutPrefix(clean, "Requires")
	}
	if ok {
		for token := range strings.SplitSeq(body, ",") {
			words := strings.Fields(token)
			switch len(words) {
			case 2:
				if n, ok := parseU32(words[0]); ok {
					set(words[1], n)
				} else if n, ok := parseU32(words[1]); ok {
					set(words[0], n)
				}
			case 1:
				if n, ok := parseU32(words[0]); ok {
					req.Level = new(n)
				}
			}
		}
	} else if k, v, found := strings.Cut(clean, ":"); found {
		if n, ok := parseU32(trimSpace(v)); ok {
			set(trimSpace(k), n)
		}
	}
}

// stripParenSuffixes removes `(augmented)`-style suffixes; lines without `(` skip the regex
// entirely.
func stripParenSuffixes(s string) string {
	if strings.Contains(s, "(") {
		return parenSuffix.ReplaceAllLiteralString(s, "")
	}
	return s
}

func firstNumberStr(s string) (string, bool) {
	m := numberRe.FindString(s)
	if m == "" {
		return "", false
	}
	return strings.ReplaceAll(m, ",", ""), true
}

func firstU32(s string) *uint32 {
	if m, ok := firstNumberStr(s); ok {
		if n, ok := parseU32(m); ok {
			return &n
		}
	}
	return nil
}

func firstF64(s string) *float64 {
	if m, ok := firstNumberStr(s); ok {
		if n, ok := parseF64(m); ok {
			return &n
		}
	}
	return nil
}

// damageAverage is the sum of the averages of every `a-b` damage range in value.
func damageAverage(value string) float64 {
	nums := ExtractValues(value)
	var total float64
	for c := range slices.Chunk(nums, 2) {
		var sum float64
		for _, v := range c {
			sum += v
		}
		total += sum / float64(len(c))
	}
	return total
}

func parsePropertyLine(item *ParsedItem, line string) {
	key, value, ok := strings.Cut(line, ": ")
	if !ok {
		item.Properties = append(item.Properties, Property{Name: line, Value: "", Augmented: false})
		return
	}
	augmented := strings.Contains(value, "(augmented)")
	clean := trimSpace(stripParenSuffixes(value))
	s := &item.Stats
	switch key {
	case "Item Level":
		item.ItemLevel = firstU32(clean)
		return
	case "Sockets":
		item.Sockets = uint8(min(len(strings.Fields(clean)), math.MaxUint8))
		return
	case "Stack Size":
		parts := strings.Split(clean, "/")
		if len(parts) >= 2 {
			current, maxV := firstU32(parts[0]), firstU32(parts[1])
			if current != nil && maxV != nil {
				item.StackSize = &StackSize{Current: *current, Max: *maxV}
			}
		}
	case "Quality":
		item.Quality = firstU32(clean)
	case "Level":
		item.GemLevel = firstU32(clean)
	case "Waystone Tier", "Map Tier":
		item.WaystoneTier = firstU32(clean)
	case "Area Level":
		item.AreaLevel = firstU32(clean)
	case "Armour":
		s.Armour = firstU32(clean)
	case "Evasion Rating":
		s.Evasion = firstU32(clean)
	case "Energy Shield":
		s.EnergyShield = firstU32(clean)
	case "Spirit":
		s.Spirit = firstU32(clean)
	case "Block chance", "Chance to Block", "Block Chance":
		s.BlockChance = firstU32(clean)
	case "Critical Hit Chance", "Critical Strike Chance":
		s.CriticalHitChance = firstF64(clean)
	case "Attacks per Second":
		s.AttacksPerSecond = firstF64(clean)
	case "Physical Damage":
		s.PhysicalDPS = new(damageAverage(clean))
	case "Elemental Damage", "Fire Damage", "Cold Damage", "Lightning Damage":
		s.ElementalDPS = new(derefOr(s.ElementalDPS, 0) + damageAverage(clean))
	case "Chaos Damage":
		s.TotalDPS = new(derefOr(s.TotalDPS, 0) + damageAverage(clean))
	}
	item.Properties = append(item.Properties, Property{Name: key, Value: clean, Augmented: augmented})
}

func derefOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func markerKind(marker string) (ModKind, bool) {
	switch marker {
	case "implicit":
		return ModImplicit, true
	case "rune", "added rune":
		return ModRune, true
	case "enchant":
		return ModEnchant, true
	case "crafted":
		return ModCrafted, true
	case "fractured":
		return ModFractured, true
	case "desecrated":
		return ModDesecrated, true
	case "sanctified":
		return ModSanctified, true
	}
	return "", false
}

func splitMarker(line string) (string, *ModKind) {
	if open := strings.LastIndex(line, " ("); open >= 0 && strings.HasSuffix(line, ")") {
		if kind, ok := markerKind(line[open+2 : len(line)-1]); ok {
			return strings.TrimRightFunc(line[:open], isSpaceRune), &kind
		}
	}
	return line, nil
}

func parseAdvancedHeader(line string) *advancedState {
	m := advancedHeader.FindStringSubmatchIndex(line)
	if m == nil {
		return nil
	}
	kindText, _ := group(line, m, advKind)
	var affix *Affix
	switch {
	case strings.Contains(kindText, "Prefix"):
		affix = new(AffixPrefix)
	case strings.Contains(kindText, "Suffix"):
		affix = new(AffixSuffix)
	case strings.Contains(kindText, "Implicit"):
		affix = new(AffixImplicit)
	}
	var kind ModKind
	switch {
	case strings.Contains(kindText, "Implicit"):
		kind = ModImplicit
	case strings.Contains(kindText, "Fractured"):
		kind = ModFractured
	case strings.Contains(kindText, "Desecrated"):
		kind = ModDesecrated
	case strings.Contains(kindText, "Crafted"):
		kind = ModCrafted
	case strings.Contains(kindText, "Enhancement") || strings.Contains(kindText, "Enchant"):
		kind = ModEnchant
	case strings.Contains(kindText, "Rune") || strings.Contains(kindText, "Augment"):
		kind = ModRune
	default:
		kind = ModExplicit
	}
	info := AdvancedModInfo{Affix: affix, Ranges: []ValueRange{}}
	if name, ok := group(line, m, advName); ok {
		info.Name = new(name)
	}
	if tier, ok := group(line, m, advTier); ok {
		if n, ok := parseU8(tier); ok {
			info.Tier = &n
		}
	}
	return &advancedState{info: info, kind: kind}
}

func parseModSection(item *ParsedItem, state *parserState, lines []string) {
	first := lines[0]
	if slices.ContainsFunc(hintPrefixes, func(p string) bool { return strings.HasPrefix(first, p) }) ||
		item.Rarity == RarityCurrency || item.Rarity == RarityGem {
		return
	}
	advanced := slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "{") })
	allMarked := !slices.ContainsFunc(lines, func(l string) bool {
		_, k := splitMarker(l)
		return !(k != nil || strings.HasPrefix(l, "Grants Skill:"))
	})
	if !advanced && !allMarked {
		if state.explicitsDone && item.Rarity == RarityUnique {
			return // flavour text
		}
		state.explicitsDone = true
	}
	if advanced {
		item.Advanced = true
		state.explicitsDone = true
	}
	state.advanced = nil
	for _, line := range lines {
		if strings.HasPrefix(line, "{") {
			state.advanced = parseAdvancedHeader(line)
			continue
		}
		if (strings.HasPrefix(line, "(") && strings.HasSuffix(line, ")")) ||
			line == "Desecrated Prefix" || line == "Desecrated Suffix" || strings.HasPrefix(line, "Unrevealed") {
			continue // reminder text / unrevealed placeholders
		}
		item.Modifiers = append(item.Modifiers, parseModLine(line, state.advanced))
	}
}

func parseModLine(line string, advanced *advancedState) Modifier {
	if l, ok := strings.CutSuffix(line, "— Unscalable Value"); ok {
		line = strings.TrimRightFunc(l, isSpaceRune)
	}
	body, marker := splitMarker(line)
	ranges := []ValueRange{}
	// Rolls (`+42(40-49)`) only appear in advanced copies; skip the regex pass otherwise.
	text := body
	if strings.Contains(body, "(") {
		text = replaceAllSubmatchFunc(rollRange, body, func(idx []int) string {
			as, _ := group(body, idx, 2)
			bs, _ := group(body, idx, 3)
			a, _ := parseF64(as)
			b, _ := parseF64(bs)
			ranges = append(ranges, ValueRange{Min: math.Min(a, b), Max: math.Max(a, b)})
			v, _ := group(body, idx, 1)
			return v
		})
	}
	var kind ModKind
	switch {
	case strings.HasPrefix(body, "Grants Skill:"):
		kind = ModSkill
	case marker != nil:
		kind = *marker
	case advanced != nil:
		kind = advanced.kind
	default:
		kind = ModExplicit
	}
	var adv *AdvancedModInfo
	if advanced != nil {
		a := advanced.info.clone()
		a.Ranges = ranges
		adv = &a
	}
	return Modifier{Template: Templatize(text), Values: ExtractValues(text), Text: text, Kind: kind, StatID: nil, Advanced: adv}
}

func finish(item *ParsedItem) {
	s := &item.Stats
	if s.AttacksPerSecond != nil {
		aps := *s.AttacksPerSecond
		round := func(v float64) float64 { return math.Round(v*100) / 100 }
		scale := func(p *float64) *float64 {
			if p == nil {
				return nil
			}
			return new(round(*p * aps))
		}
		phys, ele, chaos := scale(s.PhysicalDPS), scale(s.ElementalDPS), scale(s.TotalDPS)
		s.PhysicalDPS = phys
		s.ElementalDPS = ele
		if phys == nil && ele == nil && chaos == nil {
			s.TotalDPS = nil
		} else {
			s.TotalDPS = new(round(derefOr(phys, 0) + derefOr(ele, 0) + derefOr(chaos, 0)))
		}
	} else {
		s.PhysicalDPS = nil
		s.ElementalDPS = nil
		s.TotalDPS = nil
	}
}
