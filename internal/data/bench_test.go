package data

// Startup cost of the bundled game data: zstd decode + sha256 + JSON + index build.
//
// Bundled caches in a process-wide sync.OnceValues, so these benchmarks measure the same
// work through the decode/index API, which bypasses the cache. (Rust's bench also printed
// the cold first Bundled call; BenchmarkDecodeIndex is that cost, minus the cache.)

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/crates/data/assets"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

func benchFile(b *testing.B, name string) []byte {
	b.Helper()
	data, err := fs.ReadFile(assets.FS, name)
	if err != nil {
		b.Fatal(err)
	}
	return data
}

// benchDecode decodes the four English assets (no l10n), like the Rust bench.
func benchDecode(b *testing.B) (Manifest, Bundle) {
	b.Helper()
	m, err := ParseManifest(benchFile(b, ManifestFile))
	if err != nil {
		b.Fatal(err)
	}
	var bundle Bundle
	must := func(err error) {
		if err != nil {
			b.Fatal(err)
		}
	}
	bundle.Stats, err = DecodeAsset[[]core.StatDef](&m.Stats, benchFile(b, m.Stats.File))
	must(err)
	bundle.Bases, err = DecodeAsset[[]core.BaseDef](&m.Bases, benchFile(b, m.Bases.File))
	must(err)
	bundle.Uniques, err = DecodeAsset[[]core.UniqueDef](&m.Uniques, benchFile(b, m.Uniques.File))
	must(err)
	bundle.Statics, err = DecodeAsset[[]core.StaticDef](&m.Statics, benchFile(b, m.Statics.File))
	must(err)
	return m, bundle
}

func BenchmarkDecodeAssetStats(b *testing.B) {
	m, err := ParseManifest(benchFile(b, ManifestFile))
	if err != nil {
		b.Fatal(err)
	}
	stats := benchFile(b, m.Stats.File)
	for b.Loop() {
		if _, err := DecodeAsset[[]core.StatDef](&m.Stats, stats); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZstdStats(b *testing.B) {
	stats := benchFile(b, "stats.json.zst")
	for b.Loop() {
		if _, err := decoder().DecodeAll(stats, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSha256Stats(b *testing.B) {
	raw, err := decoder().DecodeAll(benchFile(b, "stats.json.zst"), nil)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		Sha256Hex(raw)
	}
}

func BenchmarkJSONStats(b *testing.B) {
	raw, err := decoder().DecodeAll(benchFile(b, "stats.json.zst"), nil)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		var stats []core.StatDef
		if err := json.Unmarshal(raw, &stats); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeAssetAll(b *testing.B) {
	for b.Loop() {
		benchDecode(b)
	}
}

func BenchmarkIndexBuild(b *testing.B) {
	m, bundle := benchDecode(b)
	for b.Loop() {
		FromBundle(m, bundle)
	}
}

func BenchmarkDecodeIndex(b *testing.B) {
	for b.Loop() {
		m, bundle := benchDecode(b)
		FromBundle(m, bundle)
	}
}

// BenchmarkBundledUncached is the full uncached Bundled path, l10n included.
func BenchmarkBundledUncached(b *testing.B) {
	for b.Loop() {
		if _, err := loadFS(assets.FS); err != nil {
			b.Fatal(err)
		}
	}
}
