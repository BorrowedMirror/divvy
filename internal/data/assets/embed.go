// Package assets embeds the bundled game data: manifest.json plus the zstd-compressed
// JSON assets (*.json.zst) that internal/data decodes.
// It lives next to the files because go:embed cannot reach outside its own directory.
package assets

import "embed"

// FS holds manifest.json and every *.json.zst asset at its root.
//
//go:embed manifest.json *.json.zst
var FS embed.FS
