import { expect, test } from "vitest";
import { flagFor, toggleFlag, verdictFor } from "./waystone";

const flags = { "Monsters fire # additional Projectiles": "danger", "#% increased Pack Size in Map": "good" } as const;

test("flags are looked up by template and toggle on and off", () => {
  expect(flagFor(" Monsters fire # additional Projectiles ", flags)).toBe("danger");
  expect(flagFor("Monsters have #% increased Life", flags)).toBeNull();
  const warned = toggleFlag(flags, "Monsters have #% increased Life", "warn");
  expect(warned["Monsters have #% increased Life"]).toBe("warn");
  expect(toggleFlag(warned, "Monsters have #% increased Life", "warn")["Monsters have #% increased Life"]).toBeUndefined();
  expect(toggleFlag(flags, "#% increased Pack Size in Map", null)).toEqual({ "Monsters fire # additional Projectiles": "danger" });
});

test("verdict: any danger wins, then warnings, then wanted", () => {
  expect(verdictFor(["Monsters fire # additional Projectiles", "#% increased Pack Size in Map"], flags)).toEqual({ kind: "danger", danger: 1, warn: 0, good: 1 });
  expect(verdictFor(["#% increased Pack Size in Map"], flags).kind).toBe("good");
  expect(verdictFor(["x"], flags).kind).toBe("clear");
  expect(verdictFor(["x"], { x: "warn" }).kind).toBe("warn");
});
