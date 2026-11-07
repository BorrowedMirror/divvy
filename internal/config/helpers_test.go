package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return string(b)
}

// table parses TOML into a raw document (Rust `toml::from_str::<toml::Table>`).
func table(t *testing.T, text string) map[string]any {
	t.Helper()
	doc, err := NewDocument([]byte(text))
	if err != nil {
		t.Fatalf("valid toml: %v", err)
	}
	return doc.Raw()
}

// migrated runs Migrate on a TOML document and returns the raw result.
func migrated(text string) (map[string]any, error) {
	doc, err := NewDocument([]byte(text))
	if err != nil {
		return nil, err
	}
	if err := Migrate(doc); err != nil {
		return nil, err
	}
	return doc.Raw(), nil
}

// fromTOML is `toml::from_str::<Settings>` (no migration, no validation).
func fromTOML(text string) (Settings, error) {
	doc, err := NewDocument([]byte(text))
	if err != nil {
		return Settings{}, err
	}
	return decodeDocument(doc.Raw())
}

func mustFromTOML(t *testing.T, text string) Settings {
	t.Helper()
	s, err := fromTOML(text)
	if err != nil {
		t.Fatalf("from toml: %v", err)
	}
	return s
}

// toTOML is `toml::to_string`.
func toTOML(t *testing.T, s Settings) string {
	t.Helper()
	b, err := MarshalTOML(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeSettings(t *testing.T, paths Paths, text string) {
	t.Helper()
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.SettingsFile, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("expected Invalid(%s), got %v", field, err)
	}
	if inv.Field != field {
		t.Fatalf("expected Invalid(%s), got Invalid(%s): %s", field, inv.Field, inv.Reason)
	}
}

func assertEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("not equal\n got: %#v\nwant: %#v", got, want)
	}
}

func assertSettings(t *testing.T, got, want Settings) {
	t.Helper()
	if !settingsEqual(got, want) {
		gj, _ := got.MarshalJSON()
		wj, _ := want.MarshalJSON()
		t.Fatalf("settings differ\n got: %s\nwant: %s", gj, wj)
	}
}

// settingsEqual is Rust's derived PartialEq: an empty list equals a nil one.
func settingsEqual(a, b Settings) bool {
	return reflect.DeepEqual(normalised(a), normalised(b))
}

func ptr[T any](v T) *T { return &v }

func chord(key string, mods ...platform.KeyModifier) platform.Chord {
	return platform.NewChord(key, mods...)
}
