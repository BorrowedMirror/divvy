import { describe, expect, test } from "vitest";
import i18n, { supportedLanguage } from ".";
import { de } from "./locales/de";
import { en } from "./locales/en";

type Tree = { [k: string]: string | Tree };

function leafKeys(tree: Tree, prefix = ""): string[] {
  return Object.entries(tree).flatMap(([k, v]) => (typeof v === "string" ? [`${prefix}${k}`] : leafKeys(v, `${prefix}${k}.`)));
}

describe("i18n", () => {
  test("German translates known keys", async () => {
    await i18n.changeLanguage("de");
    expect(i18n.t("overlay.search")).toBe("Suchen");
    expect(i18n.t("results.total", { count: 2 })).toBe("2 Angebote");
  });

  test("missing German keys fall back to English", async () => {
    await i18n.changeLanguage("de");
    expect(i18n.t("guidance.wayland")).toBe(en.guidance.wayland);
    expect(i18n.t("settings.contact")).toBe("Contact e-mail");
  });

  test("unknown languages fall back to English", async () => {
    await i18n.changeLanguage("fr");
    expect(i18n.t("overlay.search")).toBe("Search");
  });

  test("German has no keys that English lacks", () => {
    const enKeys = new Set(leafKeys(en as Tree));
    expect(leafKeys(de as Tree).filter((k) => !enKeys.has(k))).toEqual([]);
  });

  test("supportedLanguage normalises tags", () => {
    expect(supportedLanguage("de-AT")).toBe("de");
    expect(supportedLanguage("EN")).toBe("en");
    expect(supportedLanguage("ja")).toBe("en");
    expect(supportedLanguage(undefined)).toBe("en");
  });
});
