import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { de } from "./locales/de";
import { en } from "./locales/en";

export const LANGUAGES = [
  { code: "en", name: "English" },
  { code: "de", name: "Deutsch" },
] as const;

export function supportedLanguage(tag: string | null | undefined): string {
  const primary = (tag ?? "").toLowerCase().split("-")[0] ?? "";
  return LANGUAGES.some((l) => l.code === primary) ? primary : "en";
}

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, de: { translation: de } },
  lng: "en",
  fallbackLng: "en",
  interpolation: { escapeValue: false },
  returnNull: false,
  initAsync: false,
});

export default i18n;
