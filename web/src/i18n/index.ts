/**
 * i18n setup. Translations live in `locales/<locale>/translation.json` (Crowdin layout, see
 * `.crowdin.yml` and docs/i18n.md). `en` is the source language; every other file may be
 * partial and falls back to English key by key.
 */
import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import type en from "@i18n/locales/en/translation.json";
import { LANGUAGES, type LanguageCode } from "@i18n/languages";

export { LANGUAGES, type LanguageCode } from "@i18n/languages";

type Tree = { [k: string]: string | Tree };
export type Messages = typeof en;

const files = import.meta.glob<{ default: Tree }>("./locales/*/translation.json", { eager: true });

/** All bundled resources keyed by locale code (`pt-BR`, `zh-CN`, ...). */
export const resources: Record<string, { translation: Tree }> = Object.fromEntries(
  Object.entries(files).map(([path, mod]) => [path.split("/")[2] as string, { translation: mod.default }]),
);

/** Map any BCP-47 tag (`pt-PT`, `zh-Hant-HK`, `de-AT`) to the closest supported locale. */
export function supportedLanguage(tag: string | null | undefined): LanguageCode {
  const t = (tag ?? "").replace("_", "-").toLowerCase();
  if (!t) return "en";
  const exact = LANGUAGES.find((l) => l.code.toLowerCase() === t);
  if (exact) return exact.code;
  const [primary, ...rest] = t.split("-");
  if (primary === "zh") {
    const traditional = rest.some((p) => ["hant", "tw", "hk", "mo"].includes(p));
    return traditional ? "zh-TW" : "zh-CN";
  }
  if (primary === "pt") return "pt-BR";
  const byPrimary = LANGUAGES.find((l) => l.code.toLowerCase().split("-")[0] === primary);
  return byPrimary?.code ?? "en";
}

void i18n.use(initReactI18next).init({
  resources,
  lng: "en",
  fallbackLng: { "zh-TW": ["zh-CN", "en"], default: ["en"] },
  supportedLngs: LANGUAGES.map((l) => l.code),
  interpolation: { escapeValue: false },
  returnNull: false,
  initAsync: false,
});

export default i18n;
