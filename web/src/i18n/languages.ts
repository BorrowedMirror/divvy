/**
 * Supported UI languages. Union of what Exiled Exchange 2 and Sidekick ship plus other common
 * PoE player languages. `code` is the Crowdin/BCP-47 locale and the directory name under
 * `locales/`; `name` is the endonym shown in the language picker.
 */
export const LANGUAGES = [
  { code: "en", name: "English" },
  { code: "de", name: "Deutsch" },
  { code: "fr", name: "Français" },
  { code: "es", name: "Español" },
  { code: "pt-BR", name: "Português (Brasil)" },
  { code: "it", name: "Italiano" },
  { code: "pl", name: "Polski" },
  { code: "ru", name: "Русский" },
  { code: "uk", name: "Українська" },
  { code: "tr", name: "Türkçe" },
  { code: "ja", name: "日本語" },
  { code: "ko", name: "한국어" },
  { code: "zh-CN", name: "简体中文" },
  { code: "zh-TW", name: "繁體中文" },
  { code: "th", name: "ไทย" },
] as const;

export type LanguageCode = (typeof LANGUAGES)[number]["code"];
