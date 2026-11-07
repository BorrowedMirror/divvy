package app

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/BorrowedMirror/rust-king-ledger/web"
)

func TestTrayEveryLocaleResolvesNonEmptyLabels(t *testing.T) {
	if len(TrayLocales) != 15 {
		t.Fatalf("got %d locales", len(TrayLocales))
	}
	for _, code := range TrayLocales {
		if _, err := fs.Stat(web.Locales(), code+"/translation.json"); err != nil {
			t.Errorf("%s: %v", code, err)
		}
		l := LoadTrayLabels(code)
		for _, s := range []string{l.PriceCheck, l.Settings, l.Diagnostics, l.Quit, l.Tooltip} {
			if strings.TrimSpace(s) == "" {
				t.Errorf("%s: empty tray label", code)
			}
		}
	}
}

func TestTrayLocalesMatchEmbeddedFiles(t *testing.T) {
	dirs, err := fs.Glob(web.Locales(), "*/translation.json")
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, d := range dirs {
		codes = append(codes, strings.TrimSuffix(d, "/translation.json"))
	}
	want := slices.Sorted(slices.Values(TrayLocales))
	if !slices.Equal(codes, want) {
		t.Fatalf("embedded %q, listed %q", codes, want)
	}
}

func TestTrayEnglishMatchesTranslationFile(t *testing.T) {
	l := LoadTrayLabels("en")
	if l.Settings != "Settings…" {
		t.Fatalf("settings = %q", l.Settings)
	}
	if LoadTrayLabels("xx") != l {
		t.Fatal("unknown language falls back to en")
	}
}

func TestTrayFallsBackKeyByKey(t *testing.T) {
	chosen := map[string]any{"settings": "Einstellungen…", "quit": ""}
	en := map[string]any{"settings": "Settings…", "quit": "Quit", "diagnostics": "Diag"}
	l := ResolveTrayLabels(chosen, en)
	if l.Settings != "Einstellungen…" {
		t.Errorf("settings = %q", l.Settings)
	}
	if l.Quit != "Quit" {
		t.Errorf("empty string falls back: %q", l.Quit)
	}
	if l.Diagnostics != "Diag" {
		t.Errorf("diagnostics = %q", l.Diagnostics)
	}
	if l.Tooltip != "rkl" {
		t.Errorf("missing everywhere -> built-in: %q", l.Tooltip)
	}
	if got := ResolveTrayLabels(nil, nil).PriceCheck; got != "Price check (pinned)" {
		t.Errorf("price check = %q", got)
	}
}

func TestTrayBaseLanguageAndCaseInsensitiveMatch(t *testing.T) {
	locales := fstest.MapFS{
		"en/translation.json":    {Data: []byte(`{"tray":{"quit":"Quit","settings":"Settings"}}`)},
		"pt-BR/translation.json": {Data: []byte(`{"tray":{"quit":"Sair"}}`)},
		"de/translation.json":    {Data: []byte(`{"tray":{"quit":"Beenden"}}`)},
		"fr/translation.json":    {Data: []byte(`not json`)},
	}
	if got := trayLabelsFrom(locales, "PT-br").Quit; got != "Sair" {
		t.Errorf("case-insensitive code: %q", got)
	}
	if got := trayLabelsFrom(locales, "de_AT"); got.Quit != "Beenden" || got.Settings != "Settings" {
		t.Errorf("base language: %+v", got)
	}
	if got := trayLabelsFrom(locales, "fr").Quit; got != "Quit" {
		t.Errorf("unparsable file means English: %q", got)
	}
}
