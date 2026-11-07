package config

import (
	"fmt"
	"math"
	"strings"

	"github.com/knadh/koanf/v2"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// migrationStep upgrades a raw document in place from version i to i + 1, where i is the
// step's index in [migrations].
type migrationStep func(doc *koanf.Koanf) error

// migrations are the ordered steps; migrations[i] upgrades vi -> v(i+1). To add v2, append
// a step and bump [SettingsVersion].
var migrations = []migrationStep{migrateV0ToV1}

func init() {
	if len(migrations) != int(SettingsVersion) {
		panic("config: one migration step per version")
	}
}

// NewDocument parses settings TOML into a raw koanf document (keys as written, no
// defaults), the form [Migrate] works on.
func NewDocument(text []byte) (*koanf.Koanf, error) {
	m, err := tomlParser.Unmarshal(text)
	if err != nil {
		return nil, &ParseError{Err: err}
	}
	k := newKoanf()
	if err := k.Load(mapProvider(m), nil); err != nil {
		return nil, &ParseError{Err: err}
	}
	return k, nil
}

// Migrate upgrades a raw settings document (as parsed from TOML, before defaults) from any
// older version to [SettingsVersion], in place.
func Migrate(doc *koanf.Koanf) error {
	found, err := documentVersion(doc)
	if err != nil {
		return err
	}
	if found > SettingsVersion {
		return &TooNewError{Found: found}
	}
	for _, step := range migrations[found:] {
		if err := step(doc); err != nil {
			return err
		}
	}
	return nil
}

func documentVersion(doc *koanf.Koanf) (uint32, error) {
	if !doc.Exists("version") {
		return 0, nil
	}
	switch v := doc.Get("version").(type) {
	case int64:
		if v < 0 || v > math.MaxUint32 {
			return 0, invalidf("version", "%d is not a valid version", v)
		}
		return uint32(v), nil
	default:
		return 0, invalidf("version", "expected an integer, found %s", typeStr(v))
	}
}

// typeStr is toml's `Value::type_str`.
func typeStr(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case int64:
		return "integer"
	case float64:
		return "float"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "table"
	}
	return "datetime"
}

// section checks that the top-level key name is a table, or absent (setting a key below it
// creates it).
func section(doc *koanf.Koanf, name string) error {
	if !doc.Exists(name) {
		return nil
	}
	if _, ok := doc.Get(name).(map[string]any); !ok {
		return invalid(name, "expected a table")
	}
	return nil
}

// set is `Table::insert`: the new value replaces the old one whole (koanf's Set would merge
// a table into an existing one).
func set(doc *koanf.Koanf, path string, v any) error {
	doc.Delete(path)
	return doc.Set(path, v)
}

// migrateV0ToV1: v0 (unversioned, pre-sections) stored `league`, `hotkey` and `online_only`
// at the top level:
//
//	league = "Standard"
//	hotkey = "Ctrl+D"
//	online_only = false
//
// v1 moves them into `[league]`, `[hotkeys]` and `[trade]` (`online_only` becomes
// `sellers`). An explicit v0 league meant "always use this league", i.e. auto-detect off; an
// empty one meant auto-detect.
func migrateV0ToV1(doc *koanf.Koanf) error {
	if doc.Exists("league") {
		switch v := doc.Get("league").(type) {
		case string:
			doc.Delete("league")
			if strings.TrimSpace(v) == "" {
				if err := doc.Set(key("league", "auto_detect"), true); err != nil {
					return err
				}
			} else {
				if err := doc.Set(key("league", "auto_detect"), false); err != nil {
					return err
				}
				if err := doc.Set(key("league", "league"), v); err != nil {
					return err
				}
			}
		case map[string]any:
			// Already sectioned, merely missing a version number.
		default:
			return invalidf("league", "expected a string, found %s", typeStr(v))
		}
	}
	if doc.Exists("hotkey") {
		v := doc.Get("hotkey")
		doc.Delete("hotkey")
		raw, ok := v.(string)
		if !ok {
			return invalid("hotkey", "expected a string")
		}
		chord, err := platform.ParseChord(raw)
		if err != nil {
			return invalid("hotkey", err.Error())
		}
		if err := section(doc, "hotkeys"); err != nil {
			return err
		}
		if err := set(doc, key("hotkeys", "price_check"), chordMap(chord)); err != nil {
			return err
		}
	}
	if doc.Exists("online_only") {
		v := doc.Get("online_only")
		doc.Delete("online_only")
		online, ok := v.(bool)
		if !ok {
			return invalid("online_only", "expected a boolean")
		}
		if err := section(doc, "trade"); err != nil {
			return err
		}
		sellers := "any"
		if online {
			sellers = "online"
		}
		if err := set(doc, key("trade", "sellers"), sellers); err != nil {
			return err
		}
	}
	if err := set(doc, "version", int64(1)); err != nil {
		return fmt.Errorf("set version: %w", err)
	}
	return nil
}
