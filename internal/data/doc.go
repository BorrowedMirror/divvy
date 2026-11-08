// Package data holds the bundled game data.
//
// Assets live in internal/data/assets/ as zstd-compressed JSON (`*.json.zst`) plus a plain
// manifest.json ([Manifest]) recording schema version, source URLs and the sha256 of each
// decompressed file. They are produced by `go run ./cmd/datarefresh`.
package data
