package main

// `l10n <raw.json>`: build the item text translations for non-English clients
// (internal/data/assets/l10n.json.zst) from the game tables that tools/game-extract read out
// of an installed client, keeping the other assets as they are.
//
//   - keywords from `ClientStrings` (header keys, rarities, flags, mod-header phrases), by id;
//   - item class, base and unique names from `ItemClasses`, `BaseItemTypes` and `Words`, by row;
//   - stat lines from the stat descriptions, where each language lists its wordings in the
//     same order as English.
//
// Only names and stats RKL can search for are kept (those in the other bundled assets), so
// run this after `refresh`.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
)

// rawGame is the output of `game-extract l10n`: per data folder ("" for English, else
// e.g. "german") the rows of each table, each row holding the requested string columns
// (null where a column points nowhere), plus the decoded stat description files in order.
//
// Columns per table (byte offsets in a row): clientstrings and clientstrings2: id (0),
// text (8); itemclasses: id (0), name (8); baseitemtypes: id (0), name (32); words: text
// (4), localised text (48).
type rawGame struct {
	Folders          map[string]map[string][][]*string `json:"folders"`
	StatDescriptions []statFile                        `json:"stat_descriptions"`
}

// statFile is one stat description file, decoded to text.
type statFile struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// gameLanguage is a language whose client searches the main trade site.
type gameLanguage struct{ name, code, folder string }

var languages = []gameLanguage{
	{"German", "de", "german"},
	{"French", "fr", "french"},
	{"Spanish", "es", "spanish"},
	{"Portuguese", "pt-BR", "portuguese"},
	{"Russian", "ru", "russian"},
	{"Thai", "th", "thai"},
	{"Japanese", "ja", "japanese"},
	{"Korean", "ko", "korean"},
}

// clientKey is a `ClientStrings` id with the English the parser expects (checked against the
// English client so a renamed string is caught).
type clientKey struct{ id, english string }

var keys = []clientKey{
	{"ItemDisplayStringClass", "Item Class"},
	{"ItemDisplayStringRarity", "Rarity"},
	{"ItemDisplayStringItemLevel", "Item Level"},
	{"ItemDisplayStringSockets", "Sockets"},
	{"ItemDisplayStackSize", "Stack Size"},
	{"ItemDisplayMapTier", "Waystone Tier"},
	{"Quality", "Quality"},
	{"Level", "Level"},
	{"ItemDisplayLakeAreaLevel", "Area Level"},
	{"ItemDisplayHeistContractLevel", "Area Level"},
	{"ItemDisplayArmourArmour", "Armour"},
	{"ItemDisplayArmourEvasionRating", "Evasion Rating"},
	{"ItemDisplayArmourEnergyShield", "Energy Shield"},
	{"ItemDisplaySpiritValue", "Spirit"},
	{"ItemDisplayShieldBlockChance", "Block chance"},
	{"ItemDisplayWeaponCriticalStrikeChance", "Critical Hit Chance"},
	{"ItemDisplayWeaponAttacksPerSecond", "Attacks per Second"},
	{"ItemDisplayWeaponPhysicalDamage", "Physical Damage"},
	{"ItemDisplayWeaponElementalDamage", "Elemental Damage"},
	{"ItemDisplayWeaponFireDamage", "Fire Damage"},
	{"ItemDisplayWeaponColdDamage", "Cold Damage"},
	{"ItemDisplayWeaponLightningDamage", "Lightning Damage"},
	{"ItemDisplayWeaponChaosDamage", "Chaos Damage"},
	{"ItemDisplayWeaponReloadTime", "Reload Time"},
	{"ItemPopupRequirements", "Requirements"},
	{"ItemRequirementsLabel", "Requires"},
	{"ItemDisplayStringNote", "Note"},
	{"ItemDisplayGrantsSkill", "Grants Skill"},
	{"StrengthShort", "Str"},
	{"DexterityShort", "Dex"},
	{"IntelligenceShort", "Int"},
	{"ItemDisplayMapWaystoneDropChance", "Waystone Drop Chance"},
	{"ItemDisplayMapPack", "Pack Size"},
	{"ItemDisplayMapItemRarity", "Item Rarity"},
	{"ItemDisplayMapMagicMonsterQuantityBonus", "Magic Monsters"},
	{"ItemDisplayMapRareMonsterQuantityBonus", "Rare Monsters"},
	{"NumberOfPortalsPerWaystone", "Revives Available"},
}

var rarities = []clientKey{
	{"ItemDisplayStringNormal", "Normal"},
	{"ItemDisplayStringMagic", "Magic"},
	{"ItemDisplayStringRare", "Rare"},
	{"ItemDisplayStringUnique", "Unique"},
	{"ItemDisplayStringGem", "Gem"},
	{"ItemDisplayStringCurrency", "Currency"},
}

var flags = []clientKey{
	{"ItemPopupCorrupted", "Corrupted"},
	{"ItemPopupDoubleCorrupted", "Twice Corrupted"},
	{"ItemPopupUnidentified", "Unidentified"},
	{"ItemPopupMirrored", "Mirrored"},
	{"ItemPopupSanctified", "Sanctified"},
	{"ItemPopupFracturedItem", "Fractured Item"},
	{"ItemPopupSplit", "Split"},
	{"ItemPopupUnmodifiable", "Unmodifiable"},
	{"ItemDisplayVeiledPrefix", "Desecrated Prefix"},
	{"ItemDisplayVeiledSuffix", "Desecrated Suffix"},
}

var words = []clientKey{{"Level", "Level"}, {"StrengthShort", "Str"}, {"DexterityShort", "Dex"}, {"IntelligenceShort", "Int"}}

// patterns are applied in order, so the specific phrases come before the generic
// `Fractured {0}` ones.
var patterns = []clientKey{
	{"QualityItem", "Superior {0}"},
	{"ModDescriptionLinePrefix", "Prefix Modifier \"{0}\""},
	{"ModDescriptionLineSuffix", "Suffix Modifier \"{0}\""},
	{"ModDescriptionLineCorruptedImplicit", "Corruption Enhancement"},
	{"ModDescriptionLineEnchantmentImplicit", "Enhancement"},
	{"ModDescriptionLineImplicit", "Implicit Modifier"},
	{"ModDescriptionLineUnique", "Unique Modifier"},
	{"ModDescriptionLineTier", " (Tier: {0})"},
	{"ModDescriptionLineRank", " (Rank: {0})"},
	{"ModDescriptionLineFractured", "Fractured {0}"},
	{"ModDescriptionLineCrafted", "Crafted {0}"},
	{"ModDescriptionLineDesecrated", "Desecrated {0}"},
}

// l10nSource is the manifest's source note for the translations.
const l10nSource = "PoE2 client: ClientStrings, ItemClasses, BaseItemTypes, Words and stat descriptions per language"

var (
	markupPair = regexp.MustCompile(`\[[^\]|]*\|([^\]]*)\]`)
	markupOne  = regexp.MustCompile(`\[([^\]|]*)\]`)
	// `\d` is Unicode-aware in Rust's regex, as in internal/core.
	numOrHash   = regexp.MustCompile(`[+-]?(?:#|\p{Nd}+(?:\.\p{Nd}+)?)`)
	placeholder = regexp.MustCompile(`[+-]?\{\p{Nd}+(?::[^}]*)?\}`)
)

// stripMarkup turns `[Evasion|Evasion Rating]` into `Evasion Rating` and `[Armour]` into
// `Armour`.
func stripMarkup(text string) string {
	return markupOne.ReplaceAllString(markupPair.ReplaceAllString(text, "${1}"), "${1}")
}

// shape is what both a trade template (`+# to maximum Life`) and a description
// (`{0:+d} to maximum Life`) reduce to: every number or slot is `#`.
func shape(text string) string {
	return numOrHash.ReplaceAllLiteralString(placeholder.ReplaceAllLiteralString(text, "#"), "#")
}

// table returns the rows of a table in folder, failing when the extract lacks it.
func (r *rawGame) table(folder, name string, columns int) ([][]*string, error) {
	tables, ok := r.Folders[folder]
	if !ok {
		return nil, fmt.Errorf("the game extract has no %q data folder", folder)
	}
	rows, ok := tables[name]
	if !ok {
		return nil, fmt.Errorf("the game extract has no %s table in %q", name, folder)
	}
	for i, row := range rows {
		if len(row) != columns {
			return nil, fmt.Errorf("%s/%s row %d has %d columns, expected %d", folder, name, i, len(row), columns)
		}
	}
	return rows, nil
}

// clientStrings returns `ClientStrings` (both tables) as id -> text.
func (r *rawGame) clientStrings(folder string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range []string{"clientstrings", "clientstrings2"} {
		rows, err := r.table(folder, name, 2)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row[0] == nil || row[1] == nil {
				continue
			}
			if _, ok := out[*row[0]]; !ok {
				out[*row[0]] = *row[1]
			}
		}
	}
	return out, nil
}

// shown is the text the client shows: markup stripped, a trailing `:`/space dropped for keys.
func shown(text string, key bool) string {
	t := stripMarkup(text)
	if key {
		return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(t), ":："))
	}
	return t
}

func pick(en, local map[string]string, ids []clientKey, key bool, lang string) [][2]string {
	out := [][2]string{}
	for _, k := range ids {
		e, okE := en[k.id]
		l, okL := local[k.id]
		if !okE || !okL {
			logf("warning: %s: client string %s is missing", lang, k.id)
			continue
		}
		if e = shown(e, key); e != k.english {
			logf("warning: client string %s reads %q in English, expected %q; skipped", k.id, e, k.english)
			continue
		}
		p := [2]string{k.english, shown(l, key)}
		if p[1] != "" && !containsPair(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func containsPair(pairs [][2]string, p [2]string) bool {
	for _, q := range pairs {
		if q == p {
			return true
		}
	}
	return false
}

func sameString(a, b *string) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// pairRows pairs column enCol of en with column localCol of local row by row (same row
// count, and same ids in column idCol when idCol >= 0).
func pairRows(en, local [][]*string, idCol, enCol, localCol int, keep func(string) bool) ([][2]string, error) {
	if len(en) != len(local) {
		return nil, fmt.Errorf("row counts differ: %d vs %d", len(en), len(local))
	}
	out := [][2]string{}
	seen := map[[2]string]bool{}
	for row := range en {
		if idCol >= 0 && !sameString(en[row][idCol], local[row][idCol]) {
			return nil, fmt.Errorf("row %d ids differ", row)
		}
		e, l := en[row][enCol], local[row][localCol]
		if e == nil || l == nil || *e == "" || *l == "" || !keep(*e) {
			continue
		}
		p := [2]string{*e, stripMarkup(*l)}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// description is one `description` block: its variants per language (English under "").
type description struct {
	variants map[string][]string
}

// quoted returns the quoted text of a variant line: `1|# "{0}% increased [Armour]" negate 1`.
func quoted(line string) (string, bool) {
	q := strings.IndexByte(line, '"')
	if q < 0 {
		return "", false
	}
	start, end := q+1, q+1
	for end < len(line) && (line[end] != '"' || line[end-1] == '\\') {
		end++
	}
	s := strings.ReplaceAll(line[start:end], `\"`, `"`)
	return strings.ReplaceAll(s, `\n`, "\n"), true
}

// parseCount parses a variant count like Rust's `str::parse::<usize>`.
func parseCount(s string) (int, bool) {
	// ParseUint rejects any sign, so only Rust's single optional `+` needs trimming.
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "+"), 10, 63)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// descriptions parses a (decoded) stat description file.
func descriptions(text string) []description {
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") || text == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSpace(strings.TrimLeft(strings.TrimSuffix(l, "\r"), "\ufeff"))
	}
	pos := 0
	next := func() (string, bool) {
		if pos >= len(lines) {
			return "", false
		}
		pos++
		return lines[pos-1], true
	}
	peek := func() (string, bool) {
		if pos >= len(lines) {
			return "", false
		}
		return lines[pos], true
	}
	var out []description
	for {
		line, ok := next()
		if !ok {
			return out
		}
		if line != "description" && !strings.HasPrefix(line, "description ") {
			continue
		}
		next() // the stat ids
		desc := description{variants: map[string][]string{}}
		lang := ""
		for {
			l, ok := peek()
			if !ok {
				break
			}
			count, ok := parseCount(l)
			if !ok {
				break
			}
			next()
			variants := []string{}
			for range count {
				if v, ok := next(); ok {
					if q, ok := quoted(v); ok {
						variants = append(variants, q)
					}
				}
			}
			desc.variants[lang] = variants
			l, ok = peek()
			name, isLang := strings.CutPrefix(l, "lang ")
			if !ok || !isLang {
				break
			}
			lang = strings.Trim(name, `"`)
			next()
		}
		out = append(out, desc)
	}
}

// statPairs returns the stat line pairs for lang, kept when the English wording matches a
// searchable stat.
func statPairs(descs []description, lang string, searchable map[string]bool) [][2]string {
	out := [][2]string{}
	seen := map[string]bool{}
	for _, d := range descs {
		en, okE := d.variants[""]
		local, okL := d.variants[lang]
		if !okE || !okL || len(en) != len(local) {
			continue
		}
		for i, e := range en {
			eLines, lLines := strings.Split(e, "\n"), strings.Split(local[i], "\n")
			if len(eLines) != len(lLines) {
				continue
			}
			for j := range eLines {
				e, l := stripMarkup(eLines[j]), stripMarkup(lLines[j])
				if searchable[shape(e)] && !seen[l] {
					seen[l] = true
					out = append(out, [2]string{e, l})
				}
			}
		}
	}
	return out
}

// searchableStats returns every wording the matcher can search for, as shapes.
func searchableStats(b *data.Bundle) map[string]bool {
	out := map[string]bool{}
	for _, s := range b.Stats {
		out[shape(s.Text)] = true
		for _, t := range wordingVariants(s.Text) {
			out[shape(t)] = true
		}
		for _, a := range s.Aliases {
			out[shape(a.Text)] = true
		}
	}
	return out
}

// buildLanguages builds every language's tables from the game extract.
func buildLanguages(raw *rawGame, b *data.Bundle) ([]core.Language, error) {
	enStrings, err := raw.clientStrings("")
	if err != nil {
		return nil, err
	}
	enClasses, err := raw.table("", "itemclasses", 2)
	if err != nil {
		return nil, err
	}
	enBases, err := raw.table("", "baseitemtypes", 2)
	if err != nil {
		return nil, err
	}
	enWords, err := raw.table("", "words", 2)
	if err != nil {
		return nil, err
	}
	if len(raw.StatDescriptions) == 0 {
		return nil, errors.New("the game extract has no stat descriptions")
	}
	var descs []description
	for _, f := range raw.StatDescriptions {
		descs = append(descs, descriptions(f.Text)...)
	}
	searchable := searchableStats(b)
	names := map[string]bool{}
	for _, x := range b.Bases {
		names[x.Name] = true
	}
	for _, x := range b.Statics {
		names[x.Text] = true
	}
	uniques := map[string]bool{}
	for _, x := range b.Uniques {
		names[x.BaseType] = true
		uniques[x.Name] = true
	}
	var out []core.Language
	for _, lang := range languages {
		strs, err := raw.clientStrings(lang.folder)
		if err != nil {
			return nil, err
		}
		pairTable := func(name string, en [][]*string, idCol, enCol, localCol int, keep func(string) bool) ([][2]string, error) {
			local, err := raw.table(lang.folder, name, 2)
			if err != nil {
				return nil, err
			}
			pairs, err := pairRows(en, local, idCol, enCol, localCol, keep)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", lang.folder, name, err)
			}
			return pairs, nil
		}
		classes, err := pairTable("itemclasses", enClasses, 0, 1, 1, func(string) bool { return true })
		if err != nil {
			return nil, err
		}
		bases, err := pairTable("baseitemtypes", enBases, 0, 1, 1, func(n string) bool { return names[n] })
		if err != nil {
			return nil, err
		}
		uniqueNames, err := pairTable("words", enWords, -1, 0, 1, func(n string) bool { return uniques[n] })
		if err != nil {
			return nil, err
		}
		pats := pick(enStrings, strs, patterns, false, lang.name)
		// `Unidentified (Tier 4)`: the flag followed by the tier suffix.
		u, okU := strs["ItemPopupUnidentified"]
		t, okT := strs["ItemTierDisplay"]
		if okU && okT {
			pats = append(pats, [2]string{"Unidentified (Tier {0})", shown(u, false) + shown(t, false)})
		}
		l := core.Language{
			Name:     lang.name,
			Code:     lang.code,
			Keys:     pick(enStrings, strs, keys, true, lang.name),
			Rarities: pick(enStrings, strs, rarities, false, lang.name),
			Flags:    pick(enStrings, strs, flags, false, lang.name),
			Words:    pick(enStrings, strs, words, false, lang.name),
			Patterns: pats,
			Classes:  classes,
			Bases:    bases,
			Uniques:  uniqueNames,
			Stats:    statPairs(descs, lang.name, searchable),
		}
		logf("%s: %d keys, %d classes, %d bases, %d uniques, %d stat lines",
			lang.name, len(l.Keys), len(l.Classes), len(l.Bases), len(l.Uniques), len(l.Stats))
		out = append(out, l)
	}
	return out, nil
}

func runL10n(args []string) error {
	fs := flag.NewFlagSet("l10n", flag.ContinueOnError)
	assetsDir := fs.String("assets", "", "asset directory to update (default: internal/data/assets)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: go run ./cmd/datarefresh l10n [-assets dir] <raw.json>")
		fmt.Fprintln(fs.Output(), "\n<raw.json> is written by: cargo run --manifest-path tools/game-extract/Cargo.toml -- l10n <game_dir> <raw.json>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("l10n: name the game extract (raw.json)")
	}
	if *assetsDir == "" {
		root, err := repoRoot()
		if err != nil {
			return err
		}
		*assetsDir = filepath.Join(root, "internal", "data", "assets")
	}
	b, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var raw rawGame
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", fs.Arg(0), err)
	}
	return l10n(&raw, *assetsDir)
}

// l10n rebuilds the translations in assetsDir from raw, keeping the other assets.
func l10n(raw *rawGame, assetsDir string) error {
	current, err := data.LoadDir(assetsDir)
	if err != nil {
		return fmt.Errorf("load the current assets (run `refresh` first): %w", err)
	}
	bundle := *current.Bundle()
	if bundle.Languages, err = buildLanguages(raw, &bundle); err != nil {
		return err
	}
	m := current.Manifest
	sources := data.AssetSources{
		Stats:   m.Stats.Sources,
		Bases:   m.Bases.Sources,
		Uniques: m.Uniques.Sources,
		Statics: m.Statics.Sources,
		L10n:    []string{l10nSource},
	}
	manifest, err := data.WriteDir(assetsDir, &bundle, &sources, m.GeneratedAt, m.GameVersion)
	if err != nil {
		return err
	}
	if _, err := data.LoadDir(assetsDir); err != nil {
		return fmt.Errorf("re-loading freshly written assets: %w", err)
	}
	n := 0
	if manifest.L10n != nil {
		n = manifest.L10n.Records
	}
	logf("wrote %s (%d languages)", filepath.Join(assetsDir, "l10n.json.zst"), n)
	return nil
}
