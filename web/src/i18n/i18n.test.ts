import { describe, expect, test } from "vitest";
import i18n, { LANGUAGES, resources, supportedLanguage } from ".";
import en from "@i18n/locales/en/translation.json";

type Tree = { [k: string]: string | Tree };

function leaves(tree: Tree, prefix = ""): [string, string][] {
  return Object.entries(tree).flatMap(([k, v]) =>
    typeof v === "string" ? [[`${prefix}${k}`, v] as [string, string]] : leaves(v, `${prefix}${k}.`),
  );
}

const PLURAL = /_(zero|one|two|few|many|other)$/;
const placeholders = (s: string) => [...s.matchAll(/\{\{\s*([\w.]+)\s*\}\}/g)].map((m) => m[1]).sort();
const enLeaves = new Map(leaves(en as Tree));
const enBases = new Set([...enLeaves.keys()].filter((k) => PLURAL.test(k)).map((k) => k.replace(PLURAL, "")));
const others = LANGUAGES.filter((l) => l.code !== "en");

describe("i18n resources", () => {
  test("every supported language has a translation file and no stray files exist", () => {
    expect(Object.keys(resources).sort()).toEqual(LANGUAGES.map((l) => l.code).sort());
  });

  describe.each(others)("$code ($name)", ({ code }) => {
    const tree = resources[code]?.translation ?? {};
    const own = new Map(leaves(tree));
    const categories = new Intl.PluralRules(code).resolvedOptions().pluralCategories;

    test("has no keys that English lacks", () => {
      const unknown = [...own.keys()].filter((k) => {
        if (enLeaves.has(k)) return false;
        const base = k.replace(PLURAL, "");
        return !(PLURAL.test(k) && enBases.has(base));
      });
      expect(unknown).toEqual([]);
    });

    test("translates every key", () => {
      const missing = [...enLeaves.keys()]
        .filter((k) => !PLURAL.test(k))
        .filter((k) => !own.has(k) || own.get(k)?.trim() === "");
      expect(missing).toEqual([]);
    });

    test("uses exactly the CLDR plural forms of the language", () => {
      for (const base of enBases) {
        const forms = [...own.keys()].filter((k) => k.replace(PLURAL, "") === base && PLURAL.test(k));
        const have = forms.map((k) => k.match(PLURAL)?.[1]).sort();
        expect({ base, have }).toEqual({ base, have: [...categories].sort() });
      }
    });

    test("keeps every {{placeholder}} of the English source", () => {
      for (const [k, v] of own) {
        const source = enLeaves.get(k) ?? enLeaves.get(`${k.replace(PLURAL, "")}_other`);
        if (source === undefined) continue;
        expect({ k, p: placeholders(v) }).toEqual({ k, p: placeholders(source) });
      }
    });
  });
});

describe("i18n runtime", () => {
  test("missing keys fall back to English", async () => {
    i18n.addResource("fr", "translation", "__probe", "x");
    await i18n.changeLanguage("xx");
    expect(i18n.t("overlay.search")).toBe(en.overlay.search);
  });

  test("supportedLanguage normalises tags", () => {
    expect(supportedLanguage("de-AT")).toBe("de");
    expect(supportedLanguage("EN")).toBe("en");
    expect(supportedLanguage("pt-PT")).toBe("pt-BR");
    expect(supportedLanguage("pt_BR")).toBe("pt-BR");
    expect(supportedLanguage("zh-Hant-HK")).toBe("zh-TW");
    expect(supportedLanguage("zh-TW")).toBe("zh-TW");
    expect(supportedLanguage("zh")).toBe("zh-CN");
    expect(supportedLanguage("ja-JP")).toBe("ja");
    expect(supportedLanguage("nl")).toBe("en");
    expect(supportedLanguage(undefined)).toBe("en");
  });
});
