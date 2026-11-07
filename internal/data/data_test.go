package data

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/crates/data/assets"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

func stat(id, kind, text string, aliases ...core.StatAlias) core.StatDef {
	if aliases == nil {
		aliases = []core.StatAlias{}
	}
	return core.StatDef{ID: id, Text: text, Kind: kind, Aliases: aliases}
}

func alias(text string, negate bool) core.StatAlias {
	return core.StatAlias{Text: text, Negate: negate}
}

func base(name, category string) core.BaseDef {
	return core.BaseDef{Name: name, Category: category, ItemClass: "Helmets"}
}

func sample() Bundle {
	return Bundle{
		Stats: []core.StatDef{
			stat("explicit.life", "explicit", "# to maximum Life",
				alias("+# to maximum Life", false), alias("-# to maximum Life", true)),
			stat("explicit.ms", "explicit", "#% increased Movement Speed", alias("#% reduced Movement Speed", true)),
			stat("implicit.life", "implicit", "# to maximum Life"),
			stat("explicit.dup_a", "explicit", "Dazes on Hit"),
			stat("explicit.dup_b", "explicit", "Dazes on Hit", alias("Dazes on Hit", false)),
			stat("desecrated.life", "desecrated", "# to maximum Life"),
			stat("skill.raise", "skill", "Grants Skill: Level # Raise Shield"),
			stat("rune.life", "rune", "Bonded: # to maximum Life"),
			stat("sanctum.honour", "sanctum", "#% increased Honour restored"),
			stat("sanctum.life", "sanctum", "# to maximum Life"),
			stat("explicit.armour_local", "explicit", "#% increased Armour (Local)", alias("#% increased Armour", false)),
			stat("explicit.armour", "explicit", "#% increased Armour"),
		},
		Bases: []core.BaseDef{
			base("Hood", "armour.helmet"),
			base("Hunter Hood", "armour.helmet"),
			base("Runeforged Hunter Hood", "armour.helmet"),
			base("Gold Ring", "accessory.ring"),
		},
		Uniques: []core.UniqueDef{
			{Name: "Elevore", BaseType: "Hunter Hood"},
			{Name: "Elevore", BaseType: "Runemastered Hunter Hood"},
		},
		Statics: []core.StaticDef{{ID: "divine", Text: "Divine Orb", Group: "Currency"}},
	}
}

func sources() *AssetSources {
	return &AssetSources{
		Stats:   []string{"https://example.invalid/stats"},
		Bases:   []string{"https://example.invalid/items"},
		Uniques: []string{"https://example.invalid/items"},
		Statics: []string{"https://example.invalid/static"},
	}
}

func writeSample(t *testing.T, dir string) Manifest {
	t.Helper()
	b := sample()
	v := "0.0.1"
	m, err := WriteDir(dir, &b, sources(), "2026-01-01T00:00:00Z", &v)
	require.NoError(t, err)
	return m
}

func sampleData(t *testing.T) *GameData {
	t.Helper()
	dir := t.TempDir()
	writeSample(t, dir)
	d, err := LoadDir(dir)
	require.NoError(t, err)
	return d
}

// ---- asset encoding ----

func TestEncodeDecodeRoundTrip(t *testing.T) {
	stats := sample().Stats
	entry, b := EncodeAsset("stats.json.zst", []string{"u"}, stats)
	assert.Equal(t, len(stats), entry.Records)
	assert.Len(t, entry.Sha256, 64)
	back, err := DecodeAsset[[]core.StatDef](&entry, b)
	require.NoError(t, err)
	assert.Equal(t, stats, back)
}

func TestHashIsCaseInsensitive(t *testing.T) {
	entry, b := EncodeAsset("x", nil, []uint32{1, 2, 3})
	entry.Sha256 = strings.ToUpper(entry.Sha256)
	back, err := DecodeAsset[[]uint32](&entry, b)
	require.NoError(t, err)
	assert.Equal(t, []uint32{1, 2, 3}, back)
}

func TestSha256KnownVector(t *testing.T) {
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", Sha256Hex([]byte("abc")))
}

func TestHashMismatchRejected(t *testing.T) {
	entry, b := EncodeAsset("bases.json.zst", nil, sample().Bases)
	entry.Sha256 = strings.Repeat("0", 64)
	_, err := DecodeAsset[[]core.BaseDef](&entry, b)
	var herr *HashError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, "bases.json.zst", herr.File)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestCorruptZstdRejected(t *testing.T) {
	entry, b := EncodeAsset("stats.json.zst", nil, sample().Stats)
	_, err := DecodeAsset[[]core.StatDef](&entry, b[:len(b)/2])
	var derr *DecompressError
	require.ErrorAs(t, err, &derr, "%v", err)
	_, err = DecodeAsset[[]core.StatDef](&entry, []byte("not zstd at all"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decompress")
}

func TestWrongShapeJSONRejected(t *testing.T) {
	entry, b := EncodeAsset("statics.json.zst", nil, []int{1, 2, 3})
	_, err := DecodeAsset[[]core.StaticDef](&entry, b)
	var jerr *JSONError
	require.ErrorAs(t, err, &jerr, "%v", err)
	assert.Equal(t, "statics.json.zst", jerr.File)
	assert.Contains(t, err.Error(), "not valid JSON")
}

// ---- manifest / directories ----

func TestSchemaMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	m := writeSample(t, dir)
	m.SchemaVersion = SchemaVersion + 1
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestFile), b, 0o644))
	_, err = LoadDir(dir)
	var serr *SchemaError
	require.ErrorAs(t, err, &serr, "%v", err)
	assert.Equal(t, SchemaVersion+1, serr.Version)
	assert.Contains(t, err.Error(), "unsupported schema version")
}

func TestInvalidManifestRejected(t *testing.T) {
	_, err := ParseManifest([]byte("{}"))
	var jerr *JSONError
	require.ErrorAs(t, err, &jerr, "%v", err)
	assert.Equal(t, ManifestFile, jerr.File)
}

func TestMissingManifestRejected(t *testing.T) {
	_, err := LoadDir(t.TempDir())
	var ierr *IOError
	require.ErrorAs(t, err, &ierr, "%v", err)
	assert.Equal(t, ManifestFile, ierr.File)
	assert.Contains(t, err.Error(), "could not be read")
}

func TestMissingAssetFileRejected(t *testing.T) {
	dir := t.TempDir()
	writeSample(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, "uniques.json.zst")))
	_, err := LoadDir(dir)
	var ierr *IOError
	require.ErrorAs(t, err, &ierr, "%v", err)
	assert.Equal(t, "uniques.json.zst", ierr.File)
}

func TestTamperedAssetFileRejected(t *testing.T) {
	dir := t.TempDir()
	writeSample(t, dir)
	_, other := EncodeAsset("statics.json.zst", nil, []core.StaticDef{})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "statics.json.zst"), other, 0o644))
	_, err := LoadDir(dir)
	var herr *HashError
	assert.ErrorAs(t, err, &herr)
}

func TestWriteDirReportsIOErrors(t *testing.T) {
	b := sample()
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err := WriteDir(file, &b, sources(), "", nil)
	var ierr *IOError
	require.ErrorAs(t, err, &ierr, "%v", err)
	// Directory exists but an asset path is occupied by a directory.
	dir2 := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir2, "stats.json.zst"), 0o755))
	_, err = WriteDir(dir2, &b, sources(), "", nil)
	assert.ErrorAs(t, err, &ierr)
	dir3 := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir3, ManifestFile), 0o755))
	_, err = WriteDir(dir3, &b, sources(), "", nil)
	assert.ErrorAs(t, err, &ierr)
}

func TestLoadDirRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := writeSample(t, dir)
	d, err := LoadDir(dir)
	require.NoError(t, err)
	assert.Equal(t, m, d.Manifest)
	assert.Equal(t, sample(), *d.Bundle())
	assert.Equal(t, len(sample().Stats), m.Stats.Records)
	require.NotNil(t, m.GameVersion)
	assert.Equal(t, "0.0.1", *m.GameVersion)
}

func TestEmbeddedMissingFileRejected(t *testing.T) {
	sub := fstest.MapFS{}
	for _, name := range []string{ManifestFile, "stats.json.zst", "bases.json.zst"} {
		b, err := fs.ReadFile(assets.FS, name)
		require.NoError(t, err)
		sub[name] = &fstest.MapFile{Data: b}
	}
	_, err := loadFS(sub)
	var ierr *IOError
	require.ErrorAs(t, err, &ierr, "%v", err)
	assert.Equal(t, "uniques.json.zst", ierr.File)
}

// ---- catalog ----

type hit struct {
	id     string
	negate bool
}

func ids(hits []core.StatMatch) []hit {
	out := make([]hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, hit{h.Stat.ID, h.Negate})
	}
	return out
}

func TestStatsByTemplateDirectAliasAndKind(t *testing.T) {
	d := sampleData(t)
	assert.Equal(t, []hit{{"explicit.life", false}}, ids(d.StatsByTemplate("# to maximum Life", core.ModExplicit)))
	assert.Equal(t, []hit{{"explicit.life", false}}, ids(d.StatsByTemplate("+# to maximum Life", core.ModExplicit)))
	assert.Equal(t, []hit{{"explicit.life", true}}, ids(d.StatsByTemplate("-# to maximum Life", core.ModExplicit)))
	assert.Equal(t, []hit{{"explicit.ms", true}}, ids(d.StatsByTemplate("#% reduced Movement Speed", core.ModExplicit)))
	assert.Equal(t, []hit{{"implicit.life", false}}, ids(d.StatsByTemplate("# to maximum Life", core.ModImplicit)))
	assert.Equal(t, []hit{{"desecrated.life", false}}, ids(d.StatsByTemplate("# to maximum Life", core.ModDesecrated)))
	assert.Equal(t, []hit{{"explicit.life", false}}, ids(d.StatsByTemplate("# to maximum Life", core.ModSanctified)))
	assert.Equal(t, []hit{{"rune.life", false}}, ids(d.StatsByTemplate("Bonded: # to maximum Life", core.ModRune)))
	assert.Equal(t, []hit{{"skill.raise", false}},
		ids(d.StatsByTemplate("Grants Skill: Level # Raise Shield", core.ModSkill)))
	assert.Empty(t, d.StatsByTemplate("# to maximum Life", core.ModEnchant))
	assert.Empty(t, d.StatsByTemplate("# to maximum Life", core.ModCrafted))
	assert.Empty(t, d.StatsByTemplate("# to maximum Life", core.ModFractured))
	assert.Empty(t, d.StatsByTemplate("nope", core.ModExplicit))
}

func TestStatsByTemplateSanctumFallbackAndDirectFirst(t *testing.T) {
	d := sampleData(t)
	assert.Equal(t, []hit{{"sanctum.honour", false}},
		ids(d.StatsByTemplate("#% increased Honour restored", core.ModExplicit)))
	assert.Equal(t, []hit{{"sanctum.honour", false}},
		ids(d.StatsByTemplate("#% increased Honour restored", core.ModImplicit)))
	assert.Empty(t, d.StatsByTemplate("#% increased Honour restored", core.ModRune))
	// Fallback only when the primary kind has no hit.
	assert.Equal(t, []hit{{"explicit.life", false}}, ids(d.StatsByTemplate("# to maximum Life", core.ModExplicit)))
	// The real template is listed before stats that reach it via an alias.
	assert.Equal(t, []hit{{"explicit.armour", false}, {"explicit.armour_local", false}},
		ids(d.StatsByTemplate("#% increased Armour", core.ModExplicit)))
}

func TestStatsByTemplateAmbiguousAndDeduped(t *testing.T) {
	d := sampleData(t)
	// Two stats share a template; a self-alias must not duplicate the hit.
	assert.Equal(t, []hit{{"explicit.dup_a", false}, {"explicit.dup_b", false}},
		ids(d.StatsByTemplate("Dazes on Hit", core.ModExplicit)))
}

func TestBaseLookups(t *testing.T) {
	d := sampleData(t)
	name := func(b *core.BaseDef) string {
		t.Helper()
		require.NotNil(t, b)
		return b.Name
	}
	assert.Equal(t, "Hunter Hood", name(d.Base("Hunter Hood")))
	assert.Nil(t, d.Base("hunter hood"))
	assert.Equal(t, "Hunter Hood", name(d.BaseInMagicName("Sturdy Hunter Hood of the Fox")))
	assert.Equal(t, "Runeforged Hunter Hood", name(d.BaseInMagicName("Runeforged Hunter Hood of Ire")))
	assert.Equal(t, "Hunter Hood", name(d.BaseInMagicName("Hunter Hood")))
	assert.Equal(t, "Hood", name(d.BaseInMagicName("Shiny Hood")))
	ring := d.BaseInMagicName("Glinting Gold Ring of the Lynx")
	require.NotNil(t, ring)
	assert.Equal(t, "accessory.ring", ring.Category)
	// Whole words only: `Hoodie` must not match `Hood`.
	assert.Nil(t, d.BaseInMagicName("Cosy Hoodie"))
	assert.Nil(t, d.BaseInMagicName(""))
	// Irregular whitespace is normalised before matching word runs.
	assert.Equal(t, "Hunter Hood", name(d.BaseInMagicName("Sturdy  Hunter\tHood of the Fox")))
}

func TestUniqueAndStaticLookups(t *testing.T) {
	d := sampleData(t)
	u := d.Unique("Elevore")
	require.NotNil(t, u)
	assert.Equal(t, "Hunter Hood", u.BaseType)
	assert.Nil(t, d.Unique("Nope"))
	s := d.StaticItem("Divine Orb")
	require.NotNil(t, s)
	assert.Equal(t, "divine", s.ID)
	assert.Nil(t, d.StaticItem("Mirror"))
}

func TestCloneSharesIndexes(t *testing.T) {
	d := sampleData(t)
	c := *d
	assert.Same(t, d.Bundle(), c.Bundle())
}

// ---- bundled assets ----

func TestBundledAssetsLoad(t *testing.T) {
	d, err := Bundled()
	require.NoError(t, err)
	b := d.Bundle()
	assert.Greater(t, len(b.Stats), 5000, "%d stats", len(b.Stats))
	assert.Greater(t, len(b.Bases), 1000)
	assert.Greater(t, len(b.Uniques), 300)
	assert.Greater(t, len(b.Statics), 300)
	assert.Equal(t, SchemaVersion, d.Manifest.SchemaVersion)
	divine := d.StaticItem("Divine Orb")
	require.NotNil(t, divine)
	assert.Equal(t, "divine", divine.ID)
	hood := d.Base("Hunter Hood")
	require.NotNil(t, hood)
	assert.Equal(t, [2]string{"armour.helmet", "Helmets"}, [2]string{hood.Category, hood.ItemClass})
	magic := d.BaseInMagicName("Sturdy Hunter Hood of the Fox")
	require.NotNil(t, magic)
	assert.Equal(t, "Hunter Hood", magic.Name)
	assert.NotNil(t, d.Unique("Astramentis"))
	life := d.StatsByTemplate("+# to maximum Life", core.ModExplicit)
	assert.Contains(t, ids(life), hit{"explicit.stat_3299347043", false})
	has := func(template string, kind core.ModKind, id string) bool {
		for _, h := range d.StatsByTemplate(template, kind) {
			if h.Stat.ID == id {
				return true
			}
		}
		return false
	}
	assert.True(t, has("#% increased Armour", core.ModExplicit, "explicit.stat_1062208444"))
	assert.True(t, has("#% increased Duration", core.ModExplicit, "explicit.stat_2541588185"))
	assert.True(t, has("#% increased Honour restored", core.ModExplicit, "sanctum.stat_1583320325"))
	assert.True(t, has("#% increased Quantity of Items found in your Maps", core.ModExplicit, "explicit.stat_2390685262"))
	assert.True(t, has("#% increased Experience gain in your Maps", core.ModExplicit, "explicit.stat_57434274"))
	// Records are sorted deterministically (Rust `String` order is byte order, like Go's).
	for i := 1; i < len(b.Bases); i++ {
		assert.Less(t, b.Bases[i-1].Name, b.Bases[i].Name)
	}
}

func TestBundledIsCached(t *testing.T) {
	a := BundledStatic()
	b := BundledStatic()
	assert.Same(t, a, b)
	c, err := Bundled()
	require.NoError(t, err)
	assert.Same(t, a.Bundle(), c.Bundle())
}

// readSnapshot returns the body of an insta snapshot (after the YAML front matter).
func readSnapshot(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "crates", "data", "src", "snapshots", name))
	require.NoError(t, err)
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	rest, ok := strings.CutPrefix(s, "---\n")
	require.True(t, ok, "snapshot has front matter")
	_, body, ok := strings.Cut(rest, "\n---\n")
	require.True(t, ok, "snapshot front matter is closed")
	return []byte(body)
}

func TestBundledManifestSnapshot(t *testing.T) {
	var want any
	require.NoError(t, json.Unmarshal(readSnapshot(t, "rkl_data__tests__bundled_manifest_snapshot.snap"), &want))
	b, err := json.Marshal(BundledStatic().Manifest)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	// insta redactions: `.generated_at` and `.*.sha256`.
	got["generated_at"] = "[generated_at]"
	for _, v := range got {
		if entry, ok := v.(map[string]any); ok {
			if _, ok := entry["sha256"]; ok {
				entry["sha256"] = "[sha256]"
			}
		}
	}
	assert.Equal(t, want, any(got))
}

// Go-only: the manifest WriteDir would write for the bundled data is byte-identical to the
// one serde_json wrote.
func TestManifestFileFormatMatchesSerde(t *testing.T) {
	want, err := fs.ReadFile(assets.FS, ManifestFile)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(manifestPretty(&BundledStatic().Manifest)))
}

func TestUniquesByBaseListsEveryUniqueForABase(t *testing.T) {
	d, err := Bundled()
	require.NoError(t, err)
	var names []string
	for _, u := range d.UniquesByBase("Gold Ring") {
		names = append(names, u.Name)
	}
	assert.GreaterOrEqual(t, len(names), 2, "%v", names)
	for i := 1; i < len(names); i++ {
		assert.Less(t, names[i-1], names[i], "sorted, no duplicates: %v", names)
	}
	assert.Empty(t, d.UniquesByBase("Not A Base"))
}

// The e2e ring fixture written the way each client language would show it, from the
// bundled tables, translates back to the English fixture.
func TestEveryLanguageTranslatesTheRingFixtureBackToEnglish(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "e2e", "fixtures", "rare-ring.txt"))
	require.NoError(t, err)
	english := strings.TrimRightFunc(string(raw), unicode.IsSpace)
	d, err := Bundled()
	require.NoError(t, err)
	languages := d.Bundle().Languages
	require.Len(t, languages, 8, "German, French, Spanish, Portuguese, Russian, Thai, Japanese, Korean")
	for _, lang := range languages {
		get := func(pairs [][2]string, en string) string {
			for _, p := range pairs {
				if p[0] == en {
					return p[1]
				}
			}
			t.Fatalf("%s: no %q", lang.Name, en)
			return ""
		}
		stat := func(template, value string) string {
			return strings.ReplaceAll(get(lang.Stats, template), "{0:+d}", value)
		}
		localized := strings.Join([]string{
			get(lang.Keys, "Item Class") + ": " + get(lang.Classes, "Rings"),
			get(lang.Keys, "Rarity") + ": " + get(lang.Rarities, "Rare"),
			"Gale Loop",
			get(lang.Bases, "Sapphire Ring"),
			"--------",
			get(lang.Keys, "Requirements") + ":",
			get(lang.Keys, "Level") + ": 40",
			"--------",
			get(lang.Keys, "Item Level") + ": 75",
			"--------",
			stat("{0:+d}% to Cold Resistance", "+20") + " (implicit)",
			"--------",
			stat("{0:+d} to maximum Life", "+52"),
			stat("{0:+d}% to Fire Resistance", "+31"),
			stat("{0:+d}% to Lightning Resistance", "+12"),
		}, "\n")
		out := d.Translate(localized)
		require.NotNil(t, out, "%s: not detected", lang.Name)
		assert.Equal(t, lang.Name, out.Language)
		assert.Equal(t, english, out.Text, lang.Name)
	}
	assert.Nil(t, d.Translate(english), "English text is left alone")
}
