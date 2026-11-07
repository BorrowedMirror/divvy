package app

// Tray labels from the web UI's translation files (`tray.*` keys), so the native menu follows
// `settings.ui.language`. Missing keys fall back to English one by one; a missing, empty or
// unparsable locale file means English.

import (
	"encoding/json/v2"
	"io/fs"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/web"
)

// TrayLocales is every language in `web/src/i18n/languages.ts`; each has a
// `<code>/translation.json` in [web.Locales].
var TrayLocales = []string{"en", "de", "fr", "es", "pt-BR", "it", "pl", "ru", "uk", "tr", "ja", "ko", "zh-CN", "zh-TW", "th"}

// TrayLabels are the native tray menu's texts.
type TrayLabels struct {
	PriceCheck  string
	Settings    string
	Diagnostics string
	Quit        string
	Tooltip     string
}

// trayFallback is built-in English, used when even `en/translation.json` lacks a key.
var trayFallback = TrayLabels{
	PriceCheck:  "Price check (pinned)",
	Settings:    "Settings…",
	Diagnostics: "Diagnostics",
	Quit:        "Quit rkl",
	Tooltip:     "rkl",
}

// trayTable is the `tray` value of a locale's translation file; ok is false when the language,
// the file or the key is missing, or the file does not parse.
func trayTable(locales fs.FS, code string) (any, bool) {
	for _, c := range TrayLocales {
		if !strings.EqualFold(c, code) {
			continue
		}
		src, err := fs.ReadFile(locales, c+"/translation.json")
		if err != nil {
			return nil, false
		}
		var doc map[string]any
		if err := json.Unmarshal(src, &doc); err != nil {
			return nil, false
		}
		tray, ok := doc["tray"]
		return tray, ok
	}
	return nil, false
}

// LoadTrayLabels returns the labels for code (exact match, then the base language, e.g. `pt`
// for `pt-PT`).
func LoadTrayLabels(code string) TrayLabels {
	return trayLabelsFrom(web.Locales(), code)
}

func trayLabelsFrom(locales fs.FS, code string) TrayLabels {
	base := code
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		base = code[:i]
	}
	chosen, ok := trayTable(locales, code)
	if !ok {
		chosen, ok = trayTable(locales, base)
	}
	var chosenTable any
	if ok {
		chosenTable = chosen
	}
	en, _ := trayTable(locales, "en")
	return ResolveTrayLabels(chosenTable, en)
}

// ResolveTrayLabels resolves key by key: chosen, then en, then the built-in English; empty
// strings count as missing. chosen and en are decoded `tray` JSON values (nil when absent).
func ResolveTrayLabels(chosen, en any) TrayLabels {
	pick := func(key, fallback string) string {
		for _, t := range []any{chosen, en} {
			obj, _ := t.(map[string]any)
			if s, ok := obj[key].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
		return fallback
	}
	return TrayLabels{
		PriceCheck:  pick("priceCheck", trayFallback.PriceCheck),
		Settings:    pick("settings", trayFallback.Settings),
		Diagnostics: pick("diagnostics", trayFallback.Diagnostics),
		Quit:        pick("quit", trayFallback.Quit),
		Tooltip:     pick("tooltip", trayFallback.Tooltip),
	}
}
