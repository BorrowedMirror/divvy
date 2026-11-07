package core

import (
	"encoding/json"
	"fmt"
)

// ParseErrorKind is the variant of a [ParseError] (its serde `kind` tag).
type ParseErrorKind string

// ParseErrorKind values.
const (
	// ParseErrEmpty: input was empty or whitespace only.
	ParseErrEmpty ParseErrorKind = "empty"
	// ParseErrNotAnItem: input did not start with an `Item Class:` header (not a PoE2 item copy).
	ParseErrNotAnItem ParseErrorKind = "not_an_item"
	// ParseErrUnknownRarity: the rarity line was missing or held an unknown value.
	ParseErrUnknownRarity ParseErrorKind = "unknown_rarity"
	// ParseErrUnknownItemClass: item class header held a value we cannot map.
	ParseErrUnknownItemClass ParseErrorKind = "unknown_item_class"
	// ParseErrMalformed: structural problem with a named section.
	ParseErrMalformed ParseErrorKind = "malformed"
)

// ParseError is an error produced while parsing clipboard item text.
//
// JSON form (adjacently tagged): `{"kind":"empty"}`,
// `{"kind":"unknown_rarity","detail":"Legendary"}`,
// `{"kind":"malformed","detail":{"section":"header","reason":"..."}}`.
type ParseError struct {
	Kind ParseErrorKind
	// Value for [ParseErrUnknownRarity] and [ParseErrUnknownItemClass].
	Value string
	// Section and Reason for [ParseErrMalformed].
	Section string
	Reason  string
}

func (e ParseError) Error() string {
	switch e.Kind {
	case ParseErrEmpty:
		return "item text is empty"
	case ParseErrNotAnItem:
		return "not a Path of Exile 2 item: missing `Item Class:` header"
	case ParseErrUnknownRarity:
		return fmt.Sprintf("unknown rarity `%s`", e.Value)
	case ParseErrUnknownItemClass:
		return fmt.Sprintf("unknown item class `%s`", e.Value)
	case ParseErrMalformed:
		return fmt.Sprintf("malformed section `%s`: %s", e.Section, e.Reason)
	}
	return string(e.Kind)
}

type malformedDetail struct {
	Section string `json:"section"`
	Reason  string `json:"reason"`
}

// MarshalJSON writes the adjacently tagged serde form.
func (e ParseError) MarshalJSON() ([]byte, error) {
	buf := []byte(`{"kind":`)
	buf = appendJSONString(buf, string(e.Kind))
	switch e.Kind {
	case ParseErrUnknownRarity, ParseErrUnknownItemClass:
		buf = append(buf, `,"detail":`...)
		buf = appendJSONString(buf, e.Value)
	case ParseErrMalformed:
		buf = append(buf, `,"detail":{"section":`...)
		buf = appendJSONString(buf, e.Section)
		buf = append(buf, `,"reason":`...)
		buf = appendJSONString(buf, e.Reason)
		buf = append(buf, '}')
	}
	return append(buf, '}'), nil
}

type taggedRaw struct {
	Kind   *string          `json:"kind"`
	Detail *json.RawMessage `json:"detail"`
}

func (t taggedRaw) detail(name string, dst any) error {
	if t.Detail == nil {
		return fmt.Errorf("%s: missing field `detail`", name)
	}
	return json.Unmarshal(*t.Detail, dst)
}

// UnmarshalJSON reads the adjacently tagged serde form.
func (e *ParseError) UnmarshalJSON(data []byte) error {
	var raw taggedRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Kind == nil {
		return fmt.Errorf("ParseError: missing field `kind`")
	}
	out := ParseError{Kind: ParseErrorKind(*raw.Kind)}
	switch out.Kind {
	case ParseErrEmpty, ParseErrNotAnItem:
	case ParseErrUnknownRarity, ParseErrUnknownItemClass:
		if err := raw.detail("ParseError", &out.Value); err != nil {
			return err
		}
	case ParseErrMalformed:
		var d malformedDetail
		if err := raw.detail("ParseError", &d); err != nil {
			return err
		}
		out.Section, out.Reason = d.Section, d.Reason
	default:
		return fmt.Errorf("ParseError: unknown variant `%s`", out.Kind)
	}
	*e = out
	return nil
}

// CoreErrorKind is the variant of a [CoreError].
type CoreErrorKind string

// CoreErrorKind values.
const (
	CoreErrParse CoreErrorKind = "parse"
	// CoreErrUnpriceable: the item class cannot be priced via the requested route.
	CoreErrUnpriceable CoreErrorKind = "unpriceable"
)

// CoreError is the umbrella error for core operations other than parsing.
//
// JSON form: `{"kind":"parse","detail":{"kind":"empty"}}`, `{"kind":"unpriceable","detail":"..."}`.
type CoreError struct {
	Kind CoreErrorKind
	// Parse is set for [CoreErrParse].
	Parse *ParseError
	// Message is set for [CoreErrUnpriceable].
	Message string
}

// CoreErrorFromParse is Rust's `From<ParseError> for CoreError`.
func CoreErrorFromParse(e ParseError) CoreError { return CoreError{Kind: CoreErrParse, Parse: &e} }

func (e CoreError) Error() string {
	switch e.Kind {
	case CoreErrParse:
		if e.Parse != nil {
			return e.Parse.Error()
		}
	case CoreErrUnpriceable:
		return fmt.Sprintf("item cannot be priced: %s", e.Message)
	}
	return string(e.Kind)
}

// Unwrap exposes the wrapped [ParseError] (thiserror `transparent`).
func (e CoreError) Unwrap() error {
	if e.Kind == CoreErrParse && e.Parse != nil {
		return *e.Parse
	}
	return nil
}

// MarshalJSON writes the adjacently tagged serde form.
func (e CoreError) MarshalJSON() ([]byte, error) {
	buf := []byte(`{"kind":`)
	buf = appendJSONString(buf, string(e.Kind))
	buf = append(buf, `,"detail":`...)
	switch e.Kind {
	case CoreErrParse:
		var p ParseError
		if e.Parse != nil {
			p = *e.Parse
		}
		b, _ := p.MarshalJSON()
		buf = append(buf, b...)
	default:
		buf = appendJSONString(buf, e.Message)
	}
	return append(buf, '}'), nil
}

// UnmarshalJSON reads the adjacently tagged serde form.
func (e *CoreError) UnmarshalJSON(data []byte) error {
	var raw taggedRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Kind == nil {
		return fmt.Errorf("CoreError: missing field `kind`")
	}
	out := CoreError{Kind: CoreErrorKind(*raw.Kind)}
	switch out.Kind {
	case CoreErrParse:
		var p ParseError
		if err := raw.detail("CoreError", &p); err != nil {
			return err
		}
		out.Parse = &p
	case CoreErrUnpriceable:
		if err := raw.detail("CoreError", &out.Message); err != nil {
			return err
		}
	default:
		return fmt.Errorf("CoreError: unknown variant `%s`", out.Kind)
	}
	*e = out
	return nil
}
