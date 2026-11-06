import { expect, test } from "vitest";
import { classifyMod, parsePhrases } from "./waystone";

const lists = { avoid: ["to all maximum Resistances", "  less Recovery Rate  "], want: ["Pack Size", "maximum resistances"] };

test("classifies mods by phrase, case-insensitively, avoid first", () => {
  expect(classifyMod("Players have -20% to all maximum Resistances", lists)).toBe("avoid");
  expect(classifyMod("Players have 40% LESS RECOVERY RATE of Life and Energy Shield", lists)).toBe("avoid");
  expect(classifyMod("+12% Pack Size", lists)).toBe("want");
  expect(classifyMod("Monsters have 41% increased Life", lists)).toBeNull();
  expect(classifyMod("anything", { avoid: ["  "], want: [] })).toBeNull();
});

test("parsePhrases keeps one trimmed phrase per non-empty line", () => {
  expect(parsePhrases(" a \n\n b\n  \n")).toEqual(["a", "b"]);
});
