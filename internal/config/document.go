package config

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
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
//   - nil lists/maps become empty ones, so the TOML always has the key (`[]` / an empty
//     table);
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

// normalised returns a copy whose nil lists and maps are empty: a nil and an empty list
// mean the same setting.
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
// defaults. Fields missing from a document's sections take their defaults, but a chord has
// no defaults (both fields are required) and the waystone flags are one value: a
// `[waystone.flags]` table without the default mods means those mods are not flagged.
// koanf's default merge is recursive, so these paths need help. (Arrays — chat commands,
// stash searches, listing columns — are already replaced wholesale by koanf.)
var wholeValues = map[string]bool{
	key("hotkeys", "price_check"):        true,
	key("hotkeys", "price_check_pinned"): true,
	key("hotkeys", "open_settings"):      true,
	key("hotkeys", "open_trade"):         true,
	key("waystone", "flags"):             true,
}

// mergeOverDefaults is the koanf merge function used when a document is loaded over the
// defaults: tables merge field by field, except [wholeValues].
func mergeOverDefaults(src, dest map[string]any) error {
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

// requiredFields are the fields of the structs that have no defaults: a document that has
// one of these structs must set all of its fields.
var requiredFields = map[reflect.Type][]string{
	reflect.TypeFor[platform.Chord](): {"modifiers", "key"},
	reflect.TypeFor[ChatCommand]():    {"chord", "text", "send"},
	reflect.TypeFor[StashSearch]():    {"chord", "text"},
}

// foreignEnums check the string enums the settings use from other packages. The enums
// declared here implement [encoding.TextUnmarshaler] instead.
var foreignEnums = map[reflect.Type]func(string) error{
	reflect.TypeFor[core.PriceSourceKind](): oneOf(core.SourceTrade, core.SourcePoeNinja, core.SourcePoe2Scout),
	reflect.TypeFor[core.OnlineStatus]():    oneOf(core.StatusSecurable, core.StatusOnline, core.StatusAny),
	reflect.TypeFor[platform.KeyModifier](): oneOf(platform.Ctrl, platform.Alt, platform.Shift, platform.Meta),
	reflect.TypeFor[platform.BackendKind](): oneOf(platform.BackendWindows, platform.BackendMacOS,
		platform.BackendX11, platform.BackendWayland, platform.BackendFake),
}

// strictHook is the mapstructure decode hook that keeps decoding strict: integers must fit
// a uint32, floats are not accepted for integers, enums only take their values, and the
// [requiredFields] must be present. It runs before [mapstructure.TextUnmarshallerHookFunc],
// which decodes (and checks) the enums declared in this package.
func strictHook(_ reflect.Type, to reflect.Type, data any) (any, error) {
	for to.Kind() == reflect.Pointer {
		to = to.Elem()
	}
	v := reflect.ValueOf(data)
	if check, ok := foreignEnums[to]; ok {
		if v.Kind() != reflect.String {
			return nil, fmt.Errorf("invalid type: %s, expected a string", describe(data))
		}
		if err := check(v.String()); err != nil {
			return nil, err
		}
		return data, nil
	}
	switch to.Kind() {
	case reflect.Uint32:
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if i := v.Int(); i < 0 || i > math.MaxUint32 {
				return nil, fmt.Errorf("invalid value: integer %d is out of range for uint32", i)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if v.Uint() > math.MaxUint32 {
				return nil, fmt.Errorf("invalid value: integer %d is out of range for uint32", v.Uint())
			}
		default:
			return nil, fmt.Errorf("invalid type: %s, expected uint32", describe(data))
		}
	case reflect.Float64:
		switch v.Kind() {
		case reflect.Float32, reflect.Float64,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		default:
			return nil, fmt.Errorf("invalid type: %s, expected a number", describe(data))
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
	default:
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
		return fmt.Sprintf("boolean %t", x)
	case int64:
		return fmt.Sprintf("integer %d", x)
	case float64:
		return fmt.Sprintf("float %v", x)
	case []any:
		return "sequence"
	case map[string]any:
		return "map"
	}
	return fmt.Sprintf("%T", v)
}

// decodeDocument lays a (migrated) settings document over the defaults and decodes it.
func decodeDocument(doc map[string]any) (Settings, error) {
	k := newKoanf()
	if err := k.Load(settingsProvider{DefaultSettings()}, nil); err != nil {
		return Settings{}, err
	}
	if err := k.Load(mapProvider(doc), nil, koanf.WithMergeFunc(mergeOverDefaults)); err != nil {
		return Settings{}, err
	}
	var s Settings
	err := k.UnmarshalWithConf("", &s, koanf.UnmarshalConf{
		Tag: "koanf",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(strictHook, mapstructure.TextUnmarshallerHookFunc()),
			TagName:    "koanf",
		},
	})
	if err != nil {
		return Settings{}, err
	}
	return s, nil
}

// optionalPaths are the pointer fields: the only places JSON null is accepted.
var optionalPaths = map[string]bool{
	key("league", "league"):             true,
	key("trade", "contact"):             true,
	key("trade", "poesessid"):           true,
	key("platform", "backend_override"): true,
}

// decodeJSONDocument parses a JSON object into the same shape a TOML document has
// (integers as int64, other numbers as float64) and rejects null outside the
// [optionalPaths].
func decodeJSONDocument(b []byte) (map[string]any, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(b))
	v, err := readJSON(dec, nil)
	if err != nil {
		return nil, err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("unexpected data after the settings object: %w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid type: %s, expected a settings object", describe(v))
	}
	return m, nil
}

// readJSON reads the next JSON value from dec; path is its key path, for the null check.
func readJSON(dec *jsontext.Decoder, path []string) (any, error) {
	switch dec.PeekKind() {
	case '{':
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for dec.PeekKind() != '}' {
			tok, err := dec.ReadToken()
			if err != nil {
				return nil, err
			}
			name := tok.String()
			// Values below free-text map keys (waystone flags) are never optional, so the
			// key is safe to use in the path.
			v, err := readJSON(dec, append(slices.Clip(path), name))
			if err != nil {
				return nil, err
			}
			m[name] = v
		}
		_, err := dec.ReadToken()
		return m, err
	case '[':
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		list := []any{}
		for dec.PeekKind() != ']' {
			v, err := readJSON(dec, append(slices.Clip(path), strconv.Itoa(len(list))))
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := dec.ReadToken()
		return list, err
	default:
		return readJSONScalar(dec, path)
	}
}

// readJSONScalar reads the next JSON value from dec, which is not an object or array.
func readJSONScalar(dec *jsontext.Decoder, path []string) (any, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	switch tok.Kind() {
	case 'n':
		if len(path) > 0 && !optionalPaths[key(path...)] {
			return nil, fmt.Errorf("invalid type: null at `%s`", strings.Join(path, "."))
		}
		return nil, nil //nolint:nilnil // a JSON null
	case 't', 'f':
		return tok.Bool(), nil
	case '0':
		raw := tok.String()
		if !strings.ContainsAny(raw, ".eE") {
			if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return i, nil
			}
		}
		return tok.Float()
	default:
		return tok.String(), nil
	}
}
