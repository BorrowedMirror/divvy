package data

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data/assets"
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

// mustEncode is encodeAsset for records that always encode.
func mustEncode[T any](t *testing.T, file string, sources []string, records []T) (AssetEntry, []byte) {
	t.Helper()
	entry, b, err := encodeAsset(file, sources, records)
	require.NoError(t, err)
	return entry, b
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	stats := sample().Stats
	entry, b := mustEncode(t, "stats.json.zst", []string{"u"}, stats)
	assert.Equal(t, len(stats), entry.Records)
	assert.Len(t, entry.Sha256, 64)
	back, err := decodeAsset[[]core.StatDef](&entry, b)
	require.NoError(t, err)
	assert.Equal(t, stats, back)
}

func TestHashIsCaseInsensitive(t *testing.T) {
	entry, b := mustEncode(t, "x", nil, []uint32{1, 2, 3})
	entry.Sha256 = strings.ToUpper(entry.Sha256)
	back, err := decodeAsset[[]uint32](&entry, b)
	require.NoError(t, err)
	assert.Equal(t, []uint32{1, 2, 3}, back)
}

func TestSha256KnownVector(t *testing.T) {
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", sha256Hex([]byte("abc")))
}

func TestHashMismatchRejected(t *testing.T) {
	entry, b := mustEncode(t, "bases.json.zst", nil, sample().Bases)
	entry.Sha256 = strings.Repeat("0", 64)
	_, err := decodeAsset[[]core.BaseDef](&entry, b)
	require.ErrorIs(t, err, ErrHashMismatch)
	assertAssetError(t, err, "bases.json.zst", "verify")
}

func TestCorruptZstdRejected(t *testing.T) {
	entry, b := mustEncode(t, "stats.json.zst", nil, sample().Stats)
	_, err := decodeAsset[[]core.StatDef](&entry, b[:len(b)/2])
	assertAssetError(t, err, "stats.json.zst", "decompress")
	_, err = decodeAsset[[]core.StatDef](&entry, []byte("not zstd at all"))
	assertAssetError(t, err, "stats.json.zst", "decompress")
}

func TestWrongShapeJSONRejected(t *testing.T) {
	entry, b := mustEncode(t, "statics.json.zst", nil, []int{1, 2, 3})
	_, err := decodeAsset[[]core.StaticDef](&entry, b)
	assertAssetError(t, err, "statics.json.zst", "decode")
}

// ---- manifest / directories ----

func TestSchemaMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	m := writeSample(t, dir)
	m.SchemaVersion = schemaVersion + 1
	b, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile), b, 0o644))
	_, err = LoadDir(dir)
	require.ErrorIs(t, err, ErrSchemaVersion)
	assert.EqualError(t, err, "asset manifest.json: decode: unsupported schema version 2 (expected 1)")
}

func TestInvalidManifestRejected(t *testing.T) {
	_, err := parseManifest([]byte("not json"))
	assertAssetError(t, err, manifestFile, "decode")
	_, err = parseManifest([]byte(`{"schema_version":1}`))
	assertAssetError(t, err, manifestFile, "decode")
	_, err = parseManifest([]byte("{}"))
	require.ErrorIs(t, err, ErrSchemaVersion)
}

func TestMissingManifestRejected(t *testing.T) {
	_, err := LoadDir(t.TempDir())
	assertAssetError(t, err, manifestFile, "read")
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestMissingAssetFileRejected(t *testing.T) {
	dir := t.TempDir()
	writeSample(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, "uniques.json.zst")))
	_, err := LoadDir(dir)
	assertAssetError(t, err, "uniques.json.zst", "read")
}

func TestTamperedAssetFileRejected(t *testing.T) {
	dir := t.TempDir()
	writeSample(t, dir)
	_, other := mustEncode(t, "statics.json.zst", nil, []core.StaticDef{})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "statics.json.zst"), other, 0o644))
	_, err := LoadDir(dir)
	require.ErrorIs(t, err, ErrHashMismatch)
}

func TestWriteDirReportsIOErrors(t *testing.T) {
	b := sample()
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	_, err := WriteDir(file, &b, sources(), "", nil)
	assertAssetError(t, err, file, "write")
	// Directory exists but an asset path is occupied by a directory.
	dir2 := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir2, "stats.json.zst"), 0o755))
	_, err = WriteDir(dir2, &b, sources(), "", nil)
	assertAssetError(t, err, "stats.json.zst", "write")
	dir3 := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir3, manifestFile), 0o755))
	_, err = WriteDir(dir3, &b, sources(), "", nil)
	assertAssetError(t, err, manifestFile, "write")
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
	for _, name := range []string{manifestFile, "stats.json.zst", "bases.json.zst"} {
		b, err := fs.ReadFile(assets.FS, name)
		require.NoError(t, err)
		sub[name] = &fstest.MapFile{Data: b}
	}
	_, err := loadFS(sub)
	assertAssetError(t, err, "uniques.json.zst", "read")
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

// ---- bundled assets ----

func TestBundledAssetsLoad(t *testing.T) {
	d, err := Bundled()
	require.NoError(t, err)
	b := d.Bundle()
	assert.Greater(t, len(b.Stats), 5000, "%d stats", len(b.Stats))
	assert.Greater(t, len(b.Bases), 1000)
	assert.Greater(t, len(b.Uniques), 300)
	assert.Greater(t, len(b.Statics), 300)
	assert.Equal(t, schemaVersion, d.Manifest.SchemaVersion)
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
	// Records are sorted deterministically, by byte order.
	for i := 1; i < len(b.Bases); i++ {
		assert.Less(t, b.Bases[i-1].Name, b.Bases[i].Name)
	}
}

// bundledData is the bundled game data, which this package's tests require to be valid.
func bundledData(t *testing.T) *GameData {
	t.Helper()
	d, err := Bundled()
	require.NoError(t, err)
	return d
}

func TestBundledIsCached(t *testing.T) {
	first, second := bundledData(t), bundledData(t)
	assert.Same(t, first, second)
}

func TestBundledManifestGolden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "bundled_manifest.json"))
	require.NoError(t, err)
	m := bundledData(t).Manifest
	require.NotNil(t, m.L10n)
	l10n := *m.L10n // the cached bundle's entry must not change
	m.L10n = &l10n
	// The golden leaves out what changes on every refresh.
	m.GeneratedAt = "[generated_at]"
	for _, e := range []*AssetEntry{&m.Stats, &m.Bases, &m.Uniques, &m.Statics, m.L10n} {
		e.Sha256 = "[sha256]"
	}
	got, err := json.Marshal(m)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

// WriteDir writes the bundled manifest byte for byte as it is checked in, so a refresh
// only changes what changed.
func TestManifestFileFormatIsStable(t *testing.T) {
	want, err := fs.ReadFile(assets.FS, manifestFile)
	require.NoError(t, err)
	got, err := marshalManifest(&bundledData(t).Manifest)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

// Re-encoding the bundled records reproduces the checked-in hashes.
func TestBundledAssetsReencodeToTheSameHashes(t *testing.T) {
	d := bundledData(t)
	b := d.Bundle()
	check := func(want AssetEntry, entry AssetEntry, err error) {
		t.Helper()
		require.NoError(t, err)
		assert.Equal(t, want.Sha256, entry.Sha256, want.File)
	}
	e, _, err := encodeAsset(d.Manifest.Stats.File, nil, b.Stats)
	check(d.Manifest.Stats, e, err)
	e, _, err = encodeAsset(d.Manifest.Bases.File, nil, b.Bases)
	check(d.Manifest.Bases, e, err)
	e, _, err = encodeAsset(d.Manifest.Uniques.File, nil, b.Uniques)
	check(d.Manifest.Uniques, e, err)
	e, _, err = encodeAsset(d.Manifest.Statics.File, nil, b.Statics)
	check(d.Manifest.Statics, e, err)
	e, _, err = encodeAsset(d.Manifest.L10n.File, nil, b.Languages)
	check(*d.Manifest.L10n, e, err)
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
		out, ok := d.Translate(localized)
		require.True(t, ok, "%s: not detected", lang.Name)
		assert.Equal(t, lang.Name, out.Language)
		assert.Equal(t, english, out.Text, lang.Name)
	}
	_, ok := d.Translate(english)
	assert.False(t, ok, "English text is left alone")
}

// assertAssetError checks err is an [*AssetError] for file and op.
func assertAssetError(t *testing.T, err error, file, op string) {
	t.Helper()
	ae, ok := errors.AsType[*AssetError](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, file, ae.File)
	assert.Equal(t, op, ae.Op)
	require.Error(t, ae.Unwrap())
}
