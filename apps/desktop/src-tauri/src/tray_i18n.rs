//! Tray labels from the web UI's translation files (`tray.*` keys), so the native menu follows
//! `settings.ui.language`. Missing keys fall back to English one by one; a missing, empty or
//! unparsable locale file means English.

macro_rules! locales {
    ($($code:literal),* $(,)?) => {
        /// `(code, translation.json)` for every language in `web/src/i18n/languages.ts`.
        pub const LOCALES: &[(&str, &str)] = &[
            $(($code, include_str!(concat!("../../../../web/src/i18n/locales/", $code, "/translation.json")))),*
        ];
    };
}

locales!["en", "de", "fr", "es", "pt-BR", "it", "pl", "ru", "uk", "tr", "ja", "ko", "zh-CN", "zh-TW", "th"];

/// Built-in English, used when even `en/translation.json` lacks a key.
const FALLBACK: TrayLabels<'static> = TrayLabels {
    price_check: "Price check (pinned)",
    settings: "Settings…",
    diagnostics: "Diagnostics",
    quit: "Quit kalandra",
    tooltip: "kalandra",
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TrayLabels<'a> {
    pub price_check: &'a str,
    pub settings: &'a str,
    pub diagnostics: &'a str,
    pub quit: &'a str,
    pub tooltip: &'a str,
}

/// Owned labels for `code` (exact match, then the base language, e.g. `pt` for `pt-PT`).
pub fn labels(code: &str) -> TrayLabelsOwned {
    let tray = |code: &str| -> Option<serde_json::Value> {
        let src = LOCALES.iter().find(|(c, _)| c.eq_ignore_ascii_case(code))?.1;
        serde_json::from_str::<serde_json::Value>(src).ok()?.get("tray").cloned()
    };
    let base = code.split(['-', '_']).next().unwrap_or(code);
    let chosen = tray(code).or_else(|| tray(base));
    resolve(chosen.as_ref(), tray("en").as_ref())
}

/// Key-by-key: `chosen`, then `en`, then the built-in English; empty strings count as missing.
pub fn resolve(chosen: Option<&serde_json::Value>, en: Option<&serde_json::Value>) -> TrayLabelsOwned {
    let pick = |key: &str, fallback: &str| -> String {
        [chosen, en]
            .into_iter()
            .flatten()
            .find_map(|t| t.get(key).and_then(|v| v.as_str()).filter(|s| !s.trim().is_empty()))
            .unwrap_or(fallback)
            .to_owned()
    };
    TrayLabelsOwned {
        price_check: pick("priceCheck", FALLBACK.price_check),
        settings: pick("settings", FALLBACK.settings),
        diagnostics: pick("diagnostics", FALLBACK.diagnostics),
        quit: pick("quit", FALLBACK.quit),
        tooltip: pick("tooltip", FALLBACK.tooltip),
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TrayLabelsOwned {
    pub price_check: String,
    pub settings: String,
    pub diagnostics: String,
    pub quit: String,
    pub tooltip: String,
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::*;

    #[test]
    fn every_locale_resolves_non_empty_labels() {
        assert_eq!(LOCALES.len(), 15);
        for (code, _) in LOCALES {
            let l = labels(code);
            for s in [&l.price_check, &l.settings, &l.diagnostics, &l.quit, &l.tooltip] {
                assert!(!s.trim().is_empty(), "{code}: empty tray label");
            }
        }
    }

    #[test]
    fn english_matches_translation_file() {
        let l = labels("en");
        assert_eq!(l.settings, "Settings…");
        assert_eq!(labels("xx"), l, "unknown language falls back to en");
    }

    #[test]
    fn falls_back_key_by_key() {
        let chosen = json!({ "settings": "Einstellungen…", "quit": "" });
        let en = json!({ "settings": "Settings…", "quit": "Quit", "diagnostics": "Diag" });
        let l = resolve(Some(&chosen), Some(&en));
        assert_eq!(l.settings, "Einstellungen…");
        assert_eq!(l.quit, "Quit", "empty string falls back");
        assert_eq!(l.diagnostics, "Diag");
        assert_eq!(l.tooltip, "kalandra", "missing everywhere -> built-in");
        assert_eq!(resolve(None, None).price_check, "Price check (pinned)");
    }
}
