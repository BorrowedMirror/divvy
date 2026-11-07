package core

// serde_json-compatible JSON encoding.
//
// The frontend's TypeScript types were generated from the Rust serde derives, and the
// Rust side produced JSON with serde_json. Go's encoding/json differs in a few observable
// ways, so every wire type implements MarshalJSON through this encoder:
//
//   - f64 is written like serde_json 1.0.154 (zmij): always with a fraction or exponent
//     (`40.0`, `0.00001`, `1e-6`, `1e+16`); NaN and infinities become `null`;
//   - nil slices are `[]` and nil maps `{}` (a Rust Vec / BTreeMap is never null);
//   - []uint8 is an array of numbers, not base64;
//   - map keys are sorted (BTreeMap, and serde_json::Map without `preserve_order`);
//   - strings escape only `"`, `\` and control characters (no HTML or U+2028 escaping).
//
// Note: encoding/json.Marshal re-escapes `<`, `>`, `&`, U+2028 and U+2029 in the output of
// a MarshalJSON method unless the caller uses an Encoder with SetEscapeHTML(false). The
// result is the same JSON value either way; [MarshalSerde] returns the exact serde bytes.

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MarshalSerde encodes v exactly as serde_json would encode the equivalent Rust value.
func MarshalSerde(v any) ([]byte, error) {
	return appendValue(nil, reflect.ValueOf(v))
}

var (
	marshalerType = reflect.TypeFor[json.Marshaler]()
	numberType    = reflect.TypeFor[json.Number]()
)

// marshalStruct encodes the fields of a struct in declaration order without calling the
// struct's own MarshalJSON (which is what calls this).
func marshalStruct(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	return appendStruct(nil, rv)
}

func appendStruct(buf []byte, v reflect.Value) ([]byte, error) {
	t := v.Type()
	buf = append(buf, '{')
	first := true
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if !first {
			buf = append(buf, ',')
		}
		first = false
		buf = appendJSONString(buf, name)
		buf = append(buf, ':')
		var err error
		if buf, err = appendValue(buf, v.Field(i)); err != nil {
			return nil, err
		}
	}
	return append(buf, '}'), nil
}

func appendValue(buf []byte, v reflect.Value) ([]byte, error) {
	if !v.IsValid() {
		return append(buf, "null"...), nil
	}
	if v.Type() == numberType {
		return appendNumber(buf, json.Number(v.String()))
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return append(buf, "null"...), nil
		}
	}
	if v.Kind() != reflect.Interface && v.Type().Implements(marshalerType) {
		b, err := v.Interface().(json.Marshaler).MarshalJSON()
		if err != nil {
			return nil, err
		}
		return append(buf, b...), nil
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		return appendValue(buf, v.Elem())
	case reflect.Bool:
		return strconv.AppendBool(buf, v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(buf, v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.AppendUint(buf, v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return appendF64(buf, v.Float()), nil
	case reflect.String:
		return appendJSONString(buf, v.String()), nil
	case reflect.Slice, reflect.Array:
		buf = append(buf, '[')
		for i := range v.Len() {
			if i > 0 {
				buf = append(buf, ',')
			}
			var err error
			if buf, err = appendValue(buf, v.Index(i)); err != nil {
				return nil, err
			}
		}
		return append(buf, ']'), nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("serde: unsupported map key type %s", v.Type().Key())
		}
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		slices.Sort(keys)
		buf = append(buf, '{')
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSONString(buf, k)
			buf = append(buf, ':')
			var err error
			if buf, err = appendValue(buf, v.MapIndex(reflect.ValueOf(k).Convert(v.Type().Key()))); err != nil {
				return nil, err
			}
		}
		return append(buf, '}'), nil
	case reflect.Struct:
		return appendStruct(buf, v)
	}
	return nil, fmt.Errorf("serde: unsupported type %s", v.Type())
}

// appendNumber re-emits a decoded JSON number the way serde_json::Value would: integers
// that fit i64/u64 verbatim, everything else as f64.
func appendNumber(buf []byte, n json.Number) ([]byte, error) {
	s := string(n)
	if !strings.ContainsAny(s, ".eE") {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return strconv.AppendInt(buf, i, 10), nil
		}
		if u, err := strconv.ParseUint(s, 10, 64); err == nil {
			return strconv.AppendUint(buf, u, 10), nil
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !math.IsInf(f, 0) {
		return nil, fmt.Errorf("serde: invalid number %q", s)
	}
	return appendF64(buf, f), nil
}

// appendF64 formats like serde_json 1.0.154 (zmij): shortest round-trip digits, fixed
// notation for decimal exponents -5..=15 (always with a fraction), otherwise `d.ddde±x`.
func appendF64(buf []byte, f float64) []byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return append(buf, "null"...)
	}
	if math.Signbit(f) {
		buf = append(buf, '-')
		f = -f
	}
	if f == 0 {
		return append(buf, "0.0"...)
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±xx
	mant, expStr, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expStr)
	n := len(digits)
	switch {
	case exp >= -5 && exp <= 15 && n-1 <= exp:
		buf = append(buf, digits...)
		for range exp - (n - 1) {
			buf = append(buf, '0')
		}
		return append(buf, ".0"...)
	case exp >= 0 && exp <= 15:
		buf = append(buf, digits[:exp+1]...)
		buf = append(buf, '.')
		return append(buf, digits[exp+1:]...)
	case exp >= -5 && exp < 0:
		buf = append(buf, "0."...)
		for range -exp - 1 {
			buf = append(buf, '0')
		}
		return append(buf, digits...)
	}
	buf = append(buf, digits[0])
	if n > 1 {
		buf = append(buf, '.')
		buf = append(buf, digits[1:]...)
	}
	buf = append(buf, 'e')
	if exp < 0 {
		buf = append(buf, '-')
		exp = -exp
	} else {
		buf = append(buf, '+')
	}
	return strconv.AppendInt(buf, int64(exp), 10)
}

const hexDigits = "0123456789abcdef"

// appendJSONString escapes like serde_json: `"`, `\`, and control characters only.
func appendJSONString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch c {
			case '"':
				buf = append(buf, '\\', '"')
			case '\\':
				buf = append(buf, '\\', '\\')
			case '\b':
				buf = append(buf, '\\', 'b')
			case '\f':
				buf = append(buf, '\\', 'f')
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				if c < 0x20 {
					buf = append(buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
				} else {
					buf = append(buf, c)
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Rust strings are always valid UTF-8; mirror String::from_utf8_lossy.
			buf = append(buf, "�"...)
		} else {
			buf = append(buf, s[i:i+size]...)
		}
		i += size
	}
	return append(buf, '"')
}

// unmarshalEnum decodes a unit-variant string enum, rejecting unknown variants like serde.
func unmarshalEnum[T ~string](data []byte, dst *T, name string, variants ...T) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for _, v := range variants {
		if string(v) == s {
			*dst = v
			return nil
		}
	}
	return fmt.Errorf("%s: unknown variant `%s`", name, s)
}
