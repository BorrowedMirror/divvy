package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

// table parses TOML into a raw document.
func table(t *testing.T, text string) map[string]any {
	t.Helper()
	doc, err := NewDocument([]byte(text))
	require.NoError(t, err)
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

// fromTOML decodes TOML over the defaults, without migrating or validating.
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
	require.NoError(t, err)
	return s
}

func toTOML(t *testing.T, s Settings) string {
	t.Helper()
	b, err := MarshalTOML(s)
	require.NoError(t, err)
	return string(b)
}

func writeSettings(t *testing.T, paths Paths, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(paths.ConfigDir, 0o755))
	require.NoError(t, os.WriteFile(paths.SettingsFile, []byte(text), 0o644))
}

// requireInvalid requires err to be an [InvalidError] for field and returns it.
func requireInvalid(t *testing.T, err error, field string) *InvalidError {
	t.Helper()
	var inv *InvalidError
	require.ErrorAs(t, err, &inv)
	require.Equal(t, field, inv.Field, inv.Reason)
	return inv
}

// requireIOError requires err to be an [IOError] at path.
func requireIOError(t *testing.T, err error, path string) {
	t.Helper()
	var ioe *IOError
	require.ErrorAs(t, err, &ioe)
	assert.Equal(t, path, ioe.Path)
}

// assertSettings compares settings, treating nil and empty lists and maps as equal.
func assertSettings(t *testing.T, got, want Settings) {
	t.Helper()
	assert.Equal(t, normalised(want), normalised(got))
}

func chord(key string, mods ...platform.KeyModifier) platform.Chord {
	return platform.NewChord(key, mods...)
}
