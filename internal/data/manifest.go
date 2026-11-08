package data

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// schemaVersion is the current asset schema version. Bump on breaking asset format change.
const schemaVersion uint32 = 1

// manifestFile is the file name of the manifest within an asset directory.
const manifestFile = "manifest.json"

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

// Manifest describes an asset directory.
type Manifest struct {
	SchemaVersion uint32 `json:"schema_version"`
	// RFC 3339 generation time.
	GeneratedAt string `json:"generated_at"`
	// Game patch / data version the assets were generated for, if known (null otherwise).
	GameVersion *string    `json:"game_version"`
	Stats       AssetEntry `json:"stats"`
	Bases       AssetEntry `json:"bases"`
	Uniques     AssetEntry `json:"uniques"`
	Statics     AssetEntry `json:"statics"`
	// Item text translations for non-English game clients (`go run ./cmd/datarefresh l10n`),
	// when generated. Left out of the JSON when nil.
	L10n *AssetEntry `json:"l10n,omitzero"`
}

// parseManifest parses a manifest and checks its schema version and asset entries.
func parseManifest(b []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, &AssetError{File: manifestFile, Op: "decode", Err: err}
	}
	if m.SchemaVersion != schemaVersion {
		return Manifest{}, &AssetError{File: manifestFile, Op: "decode",
			Err: fmt.Errorf("%w %d (expected %d)", ErrSchemaVersion, m.SchemaVersion, schemaVersion)}
	}
	entries := map[string]*AssetEntry{"stats": &m.Stats, "bases": &m.Bases, "uniques": &m.Uniques, "statics": &m.Statics}
	if m.L10n != nil {
		entries["l10n"] = m.L10n
	}
	for name, e := range entries {
		if e.File == "" || e.Sha256 == "" {
			return Manifest{}, &AssetError{File: manifestFile, Op: "decode",
				Err: fmt.Errorf("entry %q has no file or sha256", name)}
		}
	}
	return m, nil
}

// marshalManifest is the manifest file content: two-space indented JSON plus a newline.
func marshalManifest(m *Manifest) ([]byte, error) {
	b, err := json.Marshal(m, jsontext.WithIndent("  "))
	if err != nil {
		return nil, &AssetError{File: manifestFile, Op: "encode", Err: err}
	}
	return append(b, '\n'), nil
}
