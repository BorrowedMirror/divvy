package app

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/web"
)

func TestTrayEveryLocaleResolvesNonEmptyLabels(t *testing.T) {
	require.Len(t, trayLocales, 15)
	for _, code := range trayLocales {
		_, err := fs.Stat(web.Locales(), code+"/translation.json")
		require.NoError(t, err, code)
		l := LoadTrayLabels(code)
		for _, s := range []string{l.PriceCheck, l.Settings, l.Diagnostics, l.Quit, l.Tooltip} {
			assert.NotEmpty(t, strings.TrimSpace(s), "%s: empty tray label", code)
		}
	}
}

func TestTrayLocalesMatchEmbeddedFiles(t *testing.T) {
	dirs, err := fs.Glob(web.Locales(), "*/translation.json")
	require.NoError(t, err)
	var codes []string
	for _, d := range dirs {
		codes = append(codes, strings.TrimSuffix(d, "/translation.json"))
	}
	assert.Equal(t, slices.Sorted(slices.Values(trayLocales)), codes)
}

func TestTrayEnglishMatchesTranslationFile(t *testing.T) {
	l := LoadTrayLabels("en")
	assert.Equal(t, "Settings…", l.Settings)
	assert.Equal(t, l, LoadTrayLabels("xx"), "unknown language falls back to en")
}

func TestTrayFallsBackKeyByKey(t *testing.T) {
	chosen := map[string]any{"settings": "Einstellungen…", "quit": ""}
	en := map[string]any{"settings": "Settings…", "quit": "Quit", "diagnostics": "Diag"}
	l := resolveTrayLabels(chosen, en)
	assert.Equal(t, "Einstellungen…", l.Settings)
	assert.Equal(t, "Quit", l.Quit, "an empty string falls back")
	assert.Equal(t, "Diag", l.Diagnostics)
	assert.Equal(t, "divvy", l.Tooltip, "missing everywhere: built-in English")
	assert.Equal(t, "Price check (pinned)", resolveTrayLabels(nil, nil).PriceCheck)
}

func TestTrayBaseLanguageAndCaseInsensitiveMatch(t *testing.T) {
	locales := fstest.MapFS{
		"en/translation.json":    {Data: []byte(`{"tray":{"quit":"Quit","settings":"Settings"}}`)},
		"pt-BR/translation.json": {Data: []byte(`{"tray":{"quit":"Sair"}}`)},
		"de/translation.json":    {Data: []byte(`{"tray":{"quit":"Beenden"}}`)},
		"fr/translation.json":    {Data: []byte(`not json`)},
	}
	assert.Equal(t, "Sair", trayLabelsFrom(locales, "PT-br").Quit, "case-insensitive code")
	de := trayLabelsFrom(locales, "de_AT")
	assert.Equal(t, "Beenden", de.Quit, "base language")
	assert.Equal(t, "Settings", de.Settings, "base language, English fallback")
	assert.Equal(t, "Quit", trayLabelsFrom(locales, "fr").Quit, "an unparsable file means English")
}
