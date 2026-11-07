package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// keyDelim separates koanf key paths. Waystone flag keys are free text that may contain
// `.`, so the usual delimiter is unsafe; the ASCII unit separator never appears in a key.
const keyDelim = "\x1f"

func key(parts ...string) string { return strings.Join(parts, keyDelim) }

// newKoanf returns an empty koanf instance with the settings key delimiter.
func newKoanf() *koanf.Koanf { return koanf.New(keyDelim) }

// mapProvider is a koanf.Provider over an already-parsed document.
type mapProvider map[string]any

func (p mapProvider) ReadBytes() ([]byte, error) { return nil, errors.New("mapProvider: no bytes") }
func (p mapProvider) Read() (map[string]any, error) {
	return map[string]any(p), nil
}

// settingsProvider is structs.Provider over a Settings value, with two fix-ups:
//   - nil lists/maps become empty ones (serde writes `[]` / an empty table, never nothing);
//   - chords use the TOML keys `modifiers` / `key`. platform.Chord has no koanf tags, so
//     fatih/structs would name them `Modifiers` / `Key`.
type settingsProvider struct{ s Settings }

func (p settingsProvider) ReadBytes() ([]byte, error) {
	return nil, errors.New("settingsProvider: no bytes")
}

func (p settingsProvider) Read() (map[string]any, error) {
	s := normalised(p.s)
	m, err := structs.Provider(s, "koanf").Read()
	if err != nil {
		return nil, err
	}
	hot, _ := m["hotkeys"].(map[string]any)
	if hot == nil {
		return nil, errors.New("settingsProvider: no hotkeys table")
	}
	hot["price_check"] = chordMap(s.Hotkeys.PriceCheck)
	hot["price_check_pinned"] = chordMap(s.Hotkeys.PriceCheckPinned)
	hot["open_settings"] = chordMap(s.Hotkeys.OpenSettings)
	hot["open_trade"] = chordMap(s.Hotkeys.OpenTrade)
	if err := fixChords(m, "chat", "commands", len(s.Chat.Commands), func(i int) platform.Chord {
		return s.Chat.Commands[i].Chord
	}); err != nil {
		return nil, err
	}
	if err := fixChords(m, "stash", "searches", len(s.Stash.Searches), func(i int) platform.Chord {
		return s.Stash.Searches[i].Chord
	}); err != nil {
		return nil, err
	}
	return m, nil
}

func fixChords(m map[string]any, section, list string, n int, chord func(int) platform.Chord) error {
	sec, _ := m[section].(map[string]any)
	items, _ := sec[list].([]any)
	if len(items) != n {
		return fmt.Errorf("settingsProvider: %s.%s has %d entries, want %d", section, list, len(items), n)
	}
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("settingsProvider: %s.%s[%d] is not a table", section, list, i)
		}
		entry["chord"] = chordMap(chord(i))
	}
	return nil
}

func chordMap(c platform.Chord) map[string]any {
	mods := make([]any, len(c.Modifiers))
	for i, m := range c.Modifiers {
		mods[i] = string(m)
	}
	return map[string]any{"modifiers": mods, "key": c.Key}
}

// normalised returns a copy whose nil lists and maps are empty, as Rust's Vec / BTreeMap
// can never be "absent".
func normalised(s Settings) Settings {
	c := s.Clone()
	fix := func(ch *platform.Chord) {
		if ch.Modifiers == nil {
			ch.Modifiers = []platform.KeyModifier{}
		}
	}
	fix(&c.Hotkeys.PriceCheck)
	fix(&c.Hotkeys.PriceCheckPinned)
	fix(&c.Hotkeys.OpenSettings)
	fix(&c.Hotkeys.OpenTrade)
	if c.Overlay.ListingColumns == nil {
		c.Overlay.ListingColumns = []ListingColumn{}
	}
	if c.Waystone.Flags == nil {
		c.Waystone.Flags = map[string]ModFlag{}
	}
	if c.Chat.Commands == nil {
		c.Chat.Commands = []ChatCommand{}
	}
	for i := range c.Chat.Commands {
		fix(&c.Chat.Commands[i].Chord)
	}
	if c.Stash.Searches == nil {
		c.Stash.Searches = []StashSearch{}
	}
	for i := range c.Stash.Searches {
		fix(&c.Stash.Searches[i].Chord)
	}
	return c
}

// wholeValues are the tables a document replaces outright instead of merging into the
// defaults. serde's `#[serde(default)]` fills in missing fields of a struct, but a Chord has
// no defaults (both fields are required) and a BTreeMap is deserialised as a whole: a
// `[waystone.flags]` table without the default mods means those mods are not flagged.
// koanf's default merge is recursive, so these paths need help. (Arrays — chat commands,
// stash searches, listing columns — are already replaced wholesale by koanf, as by serde.)
var wholeValues = map[string]bool{
	key("hotkeys", "price_check"):        true,
	key("hotkeys", "price_check_pinned"): true,
	key("hotkeys", "open_settings"):      true,
	key("hotkeys", "open_trade"):         true,
	key("waystone", "flags"):             true,
}

// serdeMerge is the koanf merge function giving serde's `#[serde(default)]` semantics when
// a document is loaded over the defaults.
func serdeMerge(src, dest map[string]any) error {
	mergeInto(nil, src, dest)
	return nil
}

func mergeInto(path []string, src, dest map[string]any) {
	for k, v := range src {
		p := append(append([]string(nil), path...), k)
		if sm, ok := v.(map[string]any); ok && !wholeValues[key(p...)] {
			if dm, ok := dest[k].(map[string]any); ok {
				mergeInto(p, sm, dm)
				continue
			}
		}
		dest[k] = v
	}
}

// requiredFields are the struct fields without a serde default, in declaration order.
var requiredFields = map[reflect.Type][]string{
	reflect.TypeFor[platform.Chord](): {"modifiers", "key"},
	reflect.TypeFor[ChatCommand]():    {"chord", "text", "send"},
	reflect.TypeFor[StashSearch]():    {"chord", "text"},
}

// strictHook makes mapstructure as strict as serde: integers must fit u32, floats are not
// accepted for integers, enums only take their variant names, and structs without
// `#[serde(default)]` need every field.
func strictHook(_ reflect.Type, to reflect.Type, data any) (any, error) {
	for to.Kind() == reflect.Pointer {
		to = to.Elem()
	}
	v := reflect.ValueOf(data)
	if names, ok := enumValues[to]; ok {
		if v.Kind() != reflect.String {
			return nil, fmt.Errorf("invalid type: %s, expected a string variant", describe(data))
		}
		if !slices.Contains(names, v.String()) {
			return nil, checkEnum(to, v.String())
		}
		return data, nil
	}
	switch to.Kind() {
	case reflect.Uint32:
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if i := v.Int(); i < 0 || i > math.MaxUint32 {
				return nil, fmt.Errorf("invalid value: integer `%d`, expected u32", i)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if v.Uint() > math.MaxUint32 {
				return nil, fmt.Errorf("invalid value: integer `%d`, expected u32", v.Uint())
			}
		default:
			return nil, fmt.Errorf("invalid type: %s, expected u32", describe(data))
		}
	case reflect.Float64:
		switch v.Kind() {
		case reflect.Float32, reflect.Float64,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		default:
			return nil, fmt.Errorf("invalid type: %s, expected f64", describe(data))
		}
	case reflect.Struct:
		if fields, ok := requiredFields[to]; ok {
			if m, ok := data.(map[string]any); ok {
				for _, f := range fields {
					if _, ok := m[f]; !ok {
						return nil, fmt.Errorf("missing field `%s`", f)
					}
				}
			}
		}
	}
	return data, nil
}

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("string %q", x)
	case bool:
		return fmt.Sprintf("boolean `%t`", x)
	case int64:
		return fmt.Sprintf("integer `%d`", x)
	case float64:
		return fmt.Sprintf("floating point `%v`", x)
	case []any:
		return "sequence"
	case map[string]any:
		return "map"
	}
	return fmt.Sprintf("%T", v)
}

// decodeDocument applies a (migrated) settings document over the defaults and decodes it,
// as serde does for `Settings` with `#[serde(default)]`.
func decodeDocument(doc map[string]any) (Settings, error) {
	k := newKoanf()
	if err := k.Load(settingsProvider{DefaultSettings()}, nil); err != nil {
		return Settings{}, err
	}
	if err := k.Load(mapProvider(doc), nil, koanf.WithMergeFunc(serdeMerge)); err != nil {
		return Settings{}, err
	}
	var s Settings
	err := k.UnmarshalWithConf("", &s, koanf.UnmarshalConf{
		Tag: "koanf",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: strictHook,
			TagName:    "koanf",
		},
	})
	if err != nil {
		return Settings{}, err
	}
	return s, nil
}

// optionPaths are the Option fields: the only places JSON `null` is accepted.
var optionPaths = map[string]bool{
	key("league", "league"):             true,
	key("trade", "contact"):             true,
	key("trade", "poesessid"):           true,
	key("platform", "backend_override"): true,
}

// decodeJSONDocument parses JSON into the same shape a TOML document has (integers as
// int64, other numbers as float64) and rejects `null` outside Option fields, as serde_json
// does.
func decodeJSONDocument(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid type: %s, expected struct Settings", describe(jsonValue(v)))
	}
	out, err := jsonValueAt(nil, m)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

func jsonValue(v any) any {
	out, _ := jsonValueAt(nil, v)
	return out
}

func jsonValueAt(path []string, v any) (any, error) {
	switch x := v.(type) {
	case nil:
		if len(path) > 0 && !optionPaths[key(path...)] {
			return nil, fmt.Errorf("invalid type: null at `%s`", strings.Join(path, "."))
		}
		return nil, nil
	case json.Number:
		s := string(x)
		if !strings.ContainsAny(s, ".eE") {
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				return i, nil
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			// Values below free-text map keys (waystone flags) are never Options; any path
			// works for the check.
			c, err := jsonValueAt(append(append([]string(nil), path...), k), e)
			if err != nil {
				return nil, err
			}
			out[k] = c
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			c, err := jsonValueAt(append(append([]string(nil), path...), strconv.Itoa(i)), e)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	}
	return v, nil
}
