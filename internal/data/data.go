// Package data holds the bundled game data.
//
// Assets live in `crates/data/assets/` as zstd-compressed JSON (`*.json.zst`) plus a
// plain `manifest.json` ([Manifest]) recording schema version, source URLs and sha256
// of each decompressed file. Assets are produced by `cargo xtask data refresh`.
package data

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/BorrowedMirror/rust-king-ledger/crates/data/assets"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// SchemaVersion is the current asset schema version. Bump on breaking asset format change.
const SchemaVersion uint32 = 1

// ManifestFile is the file name of the manifest within an asset directory.
const ManifestFile = "manifest.json"

// zstdLevel used by [EncodeAsset]. High because assets are generated rarely and shipped
// in every binary. (Rust uses zstd level 19; this is the Go encoder's strongest level.)
const zstdLevel = zstd.SpeedBestCompression

// AssetEntry is one asset file entry in the manifest.
type AssetEntry struct {
	// File name within `assets/`, e.g. `stats.json.zst`.
	File string `json:"file"`
	// Upstream URL(s) the data was derived from.
	Sources []string `json:"sources"`
	// Lowercase hex sha256 of the decompressed JSON.
	Sha256  string `json:"sha256"`
	Records int    `json:"records"`
}

type assetEntryJSON AssetEntry

// MarshalJSON writes serde-compatible JSON (`sources` is never null).
func (e AssetEntry) MarshalJSON() ([]byte, error) { return core.MarshalSerde(assetEntryJSON(e)) }

// UnmarshalJSON rejects missing or null fields, like serde.
func (e *AssetEntry) UnmarshalJSON(b []byte) error {
	if err := requireFields(b, "AssetEntry", "file", "sources", "sha256", "records"); err != nil {
		return err
	}
	return json.Unmarshal(b, (*assetEntryJSON)(e))
}

// Manifest describes an asset directory.
type Manifest struct {
	SchemaVersion uint32 `json:"schema_version"`
	// RFC 3339 generation time.
	GeneratedAt string `json:"generated_at"`
	// Game patch / data version the assets were generated for, if known.
	GameVersion *string    `json:"game_version"`
	Stats       AssetEntry `json:"stats"`
	Bases       AssetEntry `json:"bases"`
	Uniques     AssetEntry `json:"uniques"`
	Statics     AssetEntry `json:"statics"`
	// Item text translations for non-English game clients (`cargo xtask data l10n`), when
	// generated. Omitted from JSON when nil.
	L10n *AssetEntry `json:"l10n,omitempty"`
}

type manifestJSON Manifest

// manifestNoL10n is [Manifest] without the `l10n` key (serde `skip_serializing_if`).
type manifestNoL10n struct {
	SchemaVersion uint32     `json:"schema_version"`
	GeneratedAt   string     `json:"generated_at"`
	GameVersion   *string    `json:"game_version"`
	Stats         AssetEntry `json:"stats"`
	Bases         AssetEntry `json:"bases"`
	Uniques       AssetEntry `json:"uniques"`
	Statics       AssetEntry `json:"statics"`
}

// MarshalJSON writes serde-compatible JSON: `game_version` is null when unknown and
// `l10n` is left out when nil.
func (m Manifest) MarshalJSON() ([]byte, error) {
	if m.L10n != nil {
		return core.MarshalSerde(manifestJSON(m))
	}
	return core.MarshalSerde(manifestNoL10n{
		SchemaVersion: m.SchemaVersion, GeneratedAt: m.GeneratedAt, GameVersion: m.GameVersion,
		Stats: m.Stats, Bases: m.Bases, Uniques: m.Uniques, Statics: m.Statics,
	})
}

// UnmarshalJSON rejects missing or null required fields, like serde (`game_version` and
// `l10n` are optional).
func (m *Manifest) UnmarshalJSON(b []byte) error {
	err := requireFields(b, "Manifest", "schema_version", "generated_at", "stats", "bases", "uniques", "statics")
	if err != nil {
		return err
	}
	return json.Unmarshal(b, (*manifestJSON)(m))
}

// requireFields fails when b is not an object holding every key with a non-null value.
func requireFields(b []byte, typ string, keys ...string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw == nil {
		return fmt.Errorf("invalid type: null, expected struct %s", typ)
	}
	for _, k := range keys {
		v, ok := raw[k]
		if !ok {
			return fmt.Errorf("missing field `%s`", k)
		}
		if string(bytes.TrimSpace(v)) == "null" {
			return fmt.Errorf("invalid type: null for field `%s`", k)
		}
	}
	return nil
}

// DecompressError: an asset failed to decompress.
type DecompressError struct {
	File string
	Err  error
}

func (e *DecompressError) Error() string {
	return fmt.Sprintf("asset `%s` failed to decompress: %v", e.File, e.Err)
}

func (e *DecompressError) Unwrap() error { return e.Err }

// JSONError: an asset (or the manifest) is not valid JSON of the expected shape.
type JSONError struct {
	File string
	Err  error
}

func (e *JSONError) Error() string {
	return fmt.Sprintf("asset `%s` is not valid JSON: %v", e.File, e.Err)
}

func (e *JSONError) Unwrap() error { return e.Err }

// HashError: an asset's decompressed sha256 does not match the manifest.
type HashError struct {
	File, Expected, Actual string
}

func (e *HashError) Error() string {
	return fmt.Sprintf("asset `%s` hash mismatch: expected %s, got %s", e.File, e.Expected, e.Actual)
}

// SchemaError: the manifest has an unsupported schema version.
type SchemaError struct {
	Version uint32
}

func (e *SchemaError) Error() string {
	return fmt.Sprintf("unsupported schema version %d (expected %d)", e.Version, SchemaVersion)
}

// IOError: reading or writing an asset file failed (e.g. a missing file in [LoadDir]).
type IOError struct {
	File string
	Err  error
}

func (e *IOError) Error() string {
	return fmt.Sprintf("asset `%s` could not be read: %v", e.File, e.Err)
}

func (e *IOError) Unwrap() error { return e.Err }

// Bundle is the raw decoded asset bundle.
type Bundle struct {
	Stats   []core.StatDef   `json:"stats"`
	Bases   []core.BaseDef   `json:"bases"`
	Uniques []core.UniqueDef `json:"uniques"`
	Statics []core.StaticDef `json:"statics"`
	// Optional when decoding (`#[serde(default)]`).
	Languages []core.Language `json:"languages"`
}

type bundleJSON Bundle

// MarshalJSON writes serde-compatible JSON.
func (b Bundle) MarshalJSON() ([]byte, error) { return core.MarshalSerde(bundleJSON(b)) }

// Sha256Hex returns the lowercase hex sha256 of b.
func Sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// asciiEqualFold is Rust's `eq_ignore_ascii_case`.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	lower := func(c byte) byte {
		if 'A' <= c && c <= 'Z' {
			return c + ('a' - 'A')
		}
		return c
	}
	for i := range len(a) {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

// The zstd decoder and encoder are safe for concurrent DecodeAll / EncodeAll.
var (
	decoder = sync.OnceValue(func() *zstd.Decoder {
		d, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
		if err != nil {
			panic(fmt.Sprintf("zstd decoder: %v", err))
		}
		return d
	})
	encoder = sync.OnceValue(func() *zstd.Encoder {
		e, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstdLevel))
		if err != nil {
			panic(fmt.Sprintf("zstd encoder: %v", err))
		}
		return e
	})
)

// DecodeAsset decompresses, verifies and decodes one compressed asset.
func DecodeAsset[T any](entry *AssetEntry, compressed []byte) (T, error) {
	var out T
	raw, err := decoder().DecodeAll(compressed, nil)
	if err != nil {
		return out, &DecompressError{File: entry.File, Err: err}
	}
	if actual := Sha256Hex(raw); !asciiEqualFold(actual, entry.Sha256) {
		return out, &HashError{File: entry.File, Expected: entry.Sha256, Actual: actual}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, &JSONError{File: entry.File, Err: err}
	}
	return out, nil
}

// EncodeAsset encodes records as an asset: compact JSON, zstd-compressed, with a matching
// [AssetEntry]. Inverse of [DecodeAsset]; used by `xtask data refresh` and tests.
//
// Panics if the records cannot be serialised (Rust: `expect`).
func EncodeAsset[T any](file string, sources []string, records []T) (AssetEntry, []byte) {
	if records == nil {
		records = []T{}
	}
	raw, err := core.MarshalSerde(records)
	if err != nil {
		panic(fmt.Sprintf("asset records serialise to JSON: %v", err))
	}
	compressed := encoder().EncodeAll(raw, nil)
	return AssetEntry{File: file, Sources: sources, Sha256: Sha256Hex(raw), Records: len(records)}, compressed
}

// ParseManifest parses a manifest and checks its schema version.
func ParseManifest(b []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, &JSONError{File: ManifestFile, Err: err}
	}
	if m.SchemaVersion != SchemaVersion {
		return Manifest{}, &SchemaError{Version: m.SchemaVersion}
	}
	return m, nil
}

// loadWith builds [GameData] from a manifest and a function returning the compressed
// bytes of each asset file.
func loadWith(manifestBytes []byte, read func(name string) ([]byte, error)) (*GameData, error) {
	m, err := ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	var bundle Bundle
	if bundle.Stats, err = decodeFile[[]core.StatDef](&m.Stats, read); err != nil {
		return nil, err
	}
	if bundle.Bases, err = decodeFile[[]core.BaseDef](&m.Bases, read); err != nil {
		return nil, err
	}
	if bundle.Uniques, err = decodeFile[[]core.UniqueDef](&m.Uniques, read); err != nil {
		return nil, err
	}
	if bundle.Statics, err = decodeFile[[]core.StaticDef](&m.Statics, read); err != nil {
		return nil, err
	}
	if m.L10n != nil {
		if bundle.Languages, err = decodeFile[[]core.Language](m.L10n, read); err != nil {
			return nil, err
		}
	}
	return FromBundle(m, bundle), nil
}

func decodeFile[T any](entry *AssetEntry, read func(string) ([]byte, error)) (T, error) {
	b, err := read(entry.File)
	if err != nil {
		var zero T
		return zero, err
	}
	return DecodeAsset[T](entry, b)
}

// loadFS loads a manifest and its assets from the root of fsys.
func loadFS(fsys fs.FS) (*GameData, error) {
	manifest, err := fs.ReadFile(fsys, ManifestFile)
	if err != nil {
		return nil, &IOError{File: ManifestFile, Err: err}
	}
	return loadWith(manifest, func(name string) ([]byte, error) {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, &IOError{File: name, Err: err}
		}
		return b, nil
	})
}

var bundledOnce = sync.OnceValues(func() (*GameData, error) { return loadFS(assets.FS) })

// Bundled loads the assets compiled into the binary (go:embed).
//
// Decoding happens once per process; later calls return the same *GameData. Callers may
// call it on a background goroutine at startup to warm it.
func Bundled() (*GameData, error) { return bundledOnce() }

// BundledStatic returns the bundled assets as a process-wide value.
//
// Panics if the compiled-in assets are corrupt; this is a build defect caught by this
// package's tests.
func BundledStatic() *GameData {
	d, err := Bundled()
	if err != nil {
		panic(fmt.Sprintf("bundled game data assets are valid: %v", err))
	}
	return d
}

// LoadDir loads from an explicit directory (manifest + assets), for tests and updates.
func LoadDir(dir string) (*GameData, error) { return loadFS(os.DirFS(dir)) }

// AssetSources holds the upstream source URLs per asset, recorded in the manifest by
// [WriteDir].
type AssetSources struct {
	Stats, Bases, Uniques, Statics, L10n []string
}

// WriteDir writes a bundle as an asset directory (manifest + `*.json.zst`), returning the
// manifest. Used by `xtask data refresh` and tests; records are written in the given
// order.
func WriteDir(dir string, bundle *Bundle, sources *AssetSources, generatedAt string, gameVersion *string) (Manifest, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, &IOError{File: dir, Err: err}
	}
	write := func(entry AssetEntry, b []byte) (AssetEntry, error) {
		if err := os.WriteFile(filepath.Join(dir, entry.File), b, 0o644); err != nil {
			return AssetEntry{}, &IOError{File: entry.File, Err: err}
		}
		return entry, nil
	}
	m := Manifest{SchemaVersion: SchemaVersion, GeneratedAt: generatedAt, GameVersion: gameVersion}
	var err error
	if m.Stats, err = write(EncodeAsset("stats.json.zst", sources.Stats, bundle.Stats)); err != nil {
		return Manifest{}, err
	}
	if m.Bases, err = write(EncodeAsset("bases.json.zst", sources.Bases, bundle.Bases)); err != nil {
		return Manifest{}, err
	}
	if m.Uniques, err = write(EncodeAsset("uniques.json.zst", sources.Uniques, bundle.Uniques)); err != nil {
		return Manifest{}, err
	}
	if m.Statics, err = write(EncodeAsset("statics.json.zst", sources.Statics, bundle.Statics)); err != nil {
		return Manifest{}, err
	}
	if len(bundle.Languages) > 0 {
		e, err := write(EncodeAsset("l10n.json.zst", sources.L10n, bundle.Languages))
		if err != nil {
			return Manifest{}, err
		}
		m.L10n = &e
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), manifestPretty(&m), 0o644); err != nil {
		return Manifest{}, &IOError{File: ManifestFile, Err: err}
	}
	return m, nil
}

// manifestPretty is the manifest file content: serde_json pretty JSON plus a newline.
func manifestPretty(m *Manifest) []byte {
	compact, err := m.MarshalJSON()
	if err != nil {
		panic(fmt.Sprintf("manifest serialises: %v", err))
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact, "", "  "); err != nil {
		panic(fmt.Sprintf("manifest serialises: %v", err))
	}
	pretty.WriteByte('\n')
	return pretty.Bytes()
}
