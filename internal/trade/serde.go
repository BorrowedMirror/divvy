package trade

// serde-like JSON decoding.
//
// encoding/json is more lenient than serde's derived Deserialize: it matches field names
// case-insensitively, accepts a missing or null value for any field and decodes null into a
// struct as a no-op. The wire types decode through these helpers instead, so they accept and
// reject the same documents serde does: exact field names, required fields must be present
// and non-null, `#[serde(default)]` fields may be missing but not null, and Option fields
// may be missing or null. Unknown fields are ignored (no deny_unknown_fields). Error texts
// mirror serde's wording but are not byte-identical.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

type jsonObject map[string]json.RawMessage

func isNull(raw []byte) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// jsonKind names the JSON type of raw for error messages.
func jsonKind(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "nothing"
	}
	switch raw[0] {
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	case '"':
		return "string"
	case '[':
		return "sequence"
	case '{':
		return "map"
	}
	return "number"
}

// decodeObject decodes a JSON object, rejecting anything else (like serde does for a struct).
func decodeObject(data []byte, what string) (jsonObject, error) {
	if k := jsonKind(data); k != "map" {
		if k == "nothing" {
			return nil, errors.New("EOF while parsing a value")
		}
		return nil, fmt.Errorf("invalid type: %s, expected %s", k, what)
	}
	var o jsonObject
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	return o, nil
}

func fieldErr(key string, err error) error { return fmt.Errorf("field `%s`: %w", key, err) }

// required decodes a field that must be present and non-null.
func (o jsonObject) required(key string, dst any) error {
	raw, ok := o[key]
	if !ok {
		return fmt.Errorf("missing field `%s`", key)
	}
	if isNull(raw) {
		return fmt.Errorf("invalid type: null for field `%s`", key)
	}
	return unmarshalField(key, raw, dst)
}

// present decodes a serde_json::Value field: it must be present, null is a value.
func (o jsonObject) present(key string, dst *any) error {
	raw, ok := o[key]
	if !ok {
		return fmt.Errorf("missing field `%s`", key)
	}
	v, err := decodeValue(raw)
	if err != nil {
		return fieldErr(key, err)
	}
	*dst = v
	return nil
}

// defaulted decodes a `#[serde(default)]` field: may be missing, must not be null.
func (o jsonObject) defaulted(key string, dst any) error {
	raw, ok := o[key]
	if !ok {
		return nil
	}
	if isNull(raw) {
		return fmt.Errorf("invalid type: null for field `%s`", key)
	}
	return unmarshalField(key, raw, dst)
}

// optional decodes an Option field: missing or null leaves dst nil.
func (o jsonObject) optional(key string, dst any) error {
	raw, ok := o[key]
	if !ok || isNull(raw) {
		return nil
	}
	return unmarshalField(key, raw, dst)
}

// optionalValue decodes an `Option<serde_json::Value>` (null = None).
func (o jsonObject) optionalValue(key string, dst *any) error {
	raw, ok := o[key]
	if !ok || isNull(raw) {
		return nil
	}
	v, err := decodeValue(raw)
	if err != nil {
		return fieldErr(key, err)
	}
	*dst = v
	return nil
}

func unmarshalField(key string, raw []byte, dst any) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return fieldErr(key, err)
	}
	return nil
}

// decodeValue decodes any JSON document into a serde_json::Value equivalent (nil, bool,
// json.Number, string, []any, map[string]any), keeping integers exact.
func decodeValue(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("EOF while parsing a value")
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data[dec.InputOffset():])) != 0 {
		return nil, errors.New("trailing characters")
	}
	return v, nil
}

// cloneValue deep-copies a decoded JSON value.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = cloneValue(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = cloneValue(x)
		}
		return out
	}
	return v
}

// index is serde_json's `v[key]` for objects (Null when absent or not an object).
func index(v any, key string) any {
	if m, ok := v.(map[string]any); ok {
		return m[key]
	}
	return nil
}

// asArray is `Value::as_array`.
func asArray(v any) []any {
	a, _ := v.([]any)
	return a
}

// asStr is `Value::as_str`.
func asStr(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// asU64 is `Value::as_u64`: only integral numbers in u64 range (floats like `40.0` are not).
func asU64(v any) (uint64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	u, err := strconv.ParseUint(string(n), 10, 64)
	return u, err == nil
}
