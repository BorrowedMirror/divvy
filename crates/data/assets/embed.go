// Package assets embeds the bundled game data: manifest.json plus the zstd-compressed
// JSON assets (*.json.zst) that internal/data decodes.
//
// It lives next to the files because go:embed cannot reach outside its own directory and
// the Rust crate (crates/data) still builds from the same files. When the Rust tree is
// removed, this package moves with the assets (e.g. to internal/data/assets).
package assets

import "embed"

// FS holds manifest.json and every *.json.zst asset at its root.
//
//go:embed manifest.json *.json.zst
var FS embed.FS
