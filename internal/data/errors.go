package data

import (
	"errors"
	"fmt"
)

// Causes an [AssetError] can wrap besides I/O, zstd and JSON errors.
var (
	// ErrHashMismatch: an asset's decompressed sha256 does not match the manifest.
	ErrHashMismatch = errors.New("hash mismatch")
	// ErrSchemaVersion: the manifest has an unsupported schema version.
	ErrSchemaVersion = errors.New("unsupported schema version")
)

// AssetError is a failure to read, decompress, verify, decode or write one asset file (or
// the manifest).
type AssetError struct {
	// File is the asset's file name, e.g. `stats.json.zst`.
	File string
	// Op is what failed: `read`, `write`, `decompress`, `verify`, `decode` or `encode`.
	Op  string
	Err error
}

func (e *AssetError) Error() string { return fmt.Sprintf("asset %s: %s: %v", e.File, e.Op, e.Err) }

func (e *AssetError) Unwrap() error { return e.Err }
