package data

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/data/assets"
)

// Bundle is the raw decoded asset bundle.
type Bundle struct {
	Stats   []core.StatDef   `json:"stats"`
	Bases   []core.BaseDef   `json:"bases"`
	Uniques []core.UniqueDef `json:"uniques"`
	Statics []core.StaticDef `json:"statics"`
	// Languages is empty when the assets have no translations.
	Languages []core.Language `json:"languages"`
}

// loadFS loads a manifest and its assets from the root of fsys.
func loadFS(fsys fs.FS) (*GameData, error) {
	read := func(name string) ([]byte, error) {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, &AssetError{File: name, Op: "read", Err: err}
		}
		return b, nil
	}
	manifest, err := read(manifestFile)
	if err != nil {
		return nil, err
	}
	m, err := parseManifest(manifest)
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
	return newGameData(m, bundle), nil
}

func decodeFile[T any](entry *AssetEntry, read func(string) ([]byte, error)) (T, error) {
	b, err := read(entry.File)
	if err != nil {
		var zero T
		return zero, err
	}
	return decodeAsset[T](entry, b)
}

var bundled = sync.OnceValues(func() (*GameData, error) { return loadFS(assets.FS) })

// Bundled loads the assets compiled into the binary (go:embed).
//
// Decoding happens once per process; later calls return the same *GameData. Callers may
// call it on a background goroutine at startup to warm it.
func Bundled() (*GameData, error) { return bundled() }

// LoadDir loads from an explicit directory (manifest + assets), for tests and updates.
func LoadDir(dir string) (*GameData, error) { return loadFS(os.DirFS(dir)) }

// AssetSources holds the upstream source URLs per asset, recorded in the manifest by
// [WriteDir].
type AssetSources struct {
	Stats, Bases, Uniques, Statics, L10n []string
}

// WriteDir writes a bundle as an asset directory (manifest + `*.json.zst`), returning the
// manifest. Records are written in the given order; translations only when the bundle has
// languages.
func WriteDir(dir string, bundle *Bundle, sources *AssetSources, generatedAt string, gameVersion *string) (Manifest, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // internal/data/assets, committed to the repository
		return Manifest{}, &AssetError{File: dir, Op: "write", Err: err}
	}
	write := func(entry AssetEntry, b []byte, err error) (AssetEntry, error) {
		if err != nil {
			return AssetEntry{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, entry.File), b, 0o644); err != nil { //nolint:gosec // committed to the repository
			return AssetEntry{}, &AssetError{File: entry.File, Op: "write", Err: err}
		}
		return entry, nil
	}
	m := Manifest{SchemaVersion: schemaVersion, GeneratedAt: generatedAt, GameVersion: gameVersion}
	var err error
	if m.Stats, err = write(encodeAsset("stats.json.zst", sources.Stats, bundle.Stats)); err != nil {
		return Manifest{}, err
	}
	if m.Bases, err = write(encodeAsset("bases.json.zst", sources.Bases, bundle.Bases)); err != nil {
		return Manifest{}, err
	}
	if m.Uniques, err = write(encodeAsset("uniques.json.zst", sources.Uniques, bundle.Uniques)); err != nil {
		return Manifest{}, err
	}
	if m.Statics, err = write(encodeAsset("statics.json.zst", sources.Statics, bundle.Statics)); err != nil {
		return Manifest{}, err
	}
	if len(bundle.Languages) > 0 {
		e, err := write(encodeAsset("l10n.json.zst", sources.L10n, bundle.Languages))
		if err != nil {
			return Manifest{}, err
		}
		m.L10n = &e
	}
	b, err := marshalManifest(&m)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFile), b, 0o644); err != nil { //nolint:gosec // committed to the repository
		return Manifest{}, &AssetError{File: manifestFile, Op: "write", Err: err}
	}
	return m, nil
}
