package data

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// zstdLevel is the compression level of [encodeAsset]: the strongest, because assets are
// generated rarely and shipped in every binary.
const zstdLevel = zstd.SpeedBestCompression

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

// sha256Hex returns the lowercase hex sha256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// decodeAsset decompresses, verifies and decodes one compressed asset.
func decodeAsset[T any](entry *AssetEntry, compressed []byte) (T, error) {
	var out T
	raw, err := decoder().DecodeAll(compressed, nil)
	if err != nil {
		return out, &AssetError{File: entry.File, Op: "decompress", Err: err}
	}
	if actual := sha256Hex(raw); !strings.EqualFold(actual, entry.Sha256) {
		return out, &AssetError{File: entry.File, Op: "verify",
			Err: fmt.Errorf("%w: expected %s, got %s", ErrHashMismatch, entry.Sha256, actual)}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, &AssetError{File: entry.File, Op: "decode", Err: err}
	}
	return out, nil
}

// encodeAsset encodes records as an asset: compact JSON, zstd-compressed, with a matching
// [AssetEntry]. Inverse of [decodeAsset].
func encodeAsset[T any](file string, sources []string, records []T) (AssetEntry, []byte, error) {
	raw, err := json.Marshal(records, json.Deterministic(true))
	if err != nil {
		return AssetEntry{}, nil, &AssetError{File: file, Op: "encode", Err: err}
	}
	compressed := encoder().EncodeAll(raw, nil)
	return AssetEntry{File: file, Sources: sources, Sha256: sha256Hex(raw), Records: len(records)}, compressed, nil
}
