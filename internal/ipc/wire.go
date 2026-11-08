package ipc

// Wire format
//
// Wails v3 marshals bound-method results, command errors and event payloads with
// encoding/json (v1), which writes nil slices and maps as null. The frontend's types
// (web/src/bindings.ts) have no null lists or maps: a list is always an array and a map an
// object. Every payload type of this package therefore implements the encoding/json/v2
// MarshalJSONTo method (which v1 calls) through [marshalWire], so the whole payload, nested
// core, trade and config values included, is written with nil slices as [], nil maps as {},
// and map keys sorted. Optional values (pointers) stay null.
//
// Domain values a command returns directly (core.PriceSummary, []trade.League, ...) are
// built non-nil by their producers instead; payloads_test.go checks both.

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// wireOptions are the encoding options of every payload.
var wireOptions = json.JoinOptions(
	json.FormatNilSliceAsNull(false),
	json.FormatNilMapAsNull(false),
	json.Deterministic(true),
)

// marshalWire encodes v (a payload converted to a method-less type) in the wire format.
func marshalWire[T any](enc *jsontext.Encoder, v T) error {
	return json.MarshalEncode(enc, v, wireOptions)
}
