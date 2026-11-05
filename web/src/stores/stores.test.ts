import { describe, expect, test } from "vitest";
import { filterSet, ipcError, itemCopied, priceSummary, settings } from "../test/fixtures";
import { useOverlayStore } from "./overlay";
import { isDirty, useSettingsStore } from "./settings";

describe("overlay store", () => {
  const s = () => useOverlayStore.getState();

  test("itemCopied resets results and adopts pinned", () => {
    s().priceReady(priceSummary());
    s().itemCopied(itemCopied({ pinned: true }));
    expect(s().item?.base_type).toBe("Expert Hunter Hood");
    expect(s().summary).toBeNull();
    expect(s().phase).toBe("idle");
    expect(s().pinned).toBe(true);
  });

  test("filter edits are immutable patches", () => {
    s().itemCopied(itemCopied());
    const before = s().filters;
    s().updateStat(1, { enabled: true, min: 30 });
    s().updateRange("quality", { enabled: true, min: 15 });
    s().setCorrupted(false);
    s().setStatus("any");
    s().setLeague("Hardcore");
    const f = s().filters;
    expect(f).not.toBe(before);
    expect(f?.stats[1]).toMatchObject({ enabled: true, min: 30 });
    expect(f?.stats[0]?.min).toBe(76);
    expect(f?.misc.quality).toEqual({ enabled: true, min: 15, max: null });
    expect(f?.misc.corrupted).toBe(false);
    expect(f?.status).toBe("any");
    expect(f?.league).toBe("Hardcore");
  });

  test("widen applies local rewiden", () => {
    s().itemCopied(itemCopied());
    s().widen(0);
    expect(s().filters?.stats.map((x) => x.min)).toEqual([85, 32]);
    expect(s().filters?.widen_pct).toBe(0);
  });

  test("edits without filters are no-ops", () => {
    s().updateStat(0, { enabled: false });
    s().widen(5);
    expect(s().filters).toBeNull();
  });

  test("query lifecycle and rate-limit deadline", () => {
    s().itemCopied(itemCopied({ filters: filterSet() }));
    s().queryStarted();
    expect(s().phase).toBe("loading");
    s().priceFailed(ipcError("rate_limited", "slow", 5000), 1000);
    expect(s().phase).toBe("error");
    expect(s().retryAt).toBe(6000);
    s().priceFailed(ipcError("trade"), 1000);
    expect(s().retryAt).toBeNull();
    s().priceReady(priceSummary());
    expect(s().phase).toBe("ready");
    expect(s().error).toBeNull();
  });

  test("overlay state + pin", () => {
    s().setPinned(true);
    expect(s().pinned).toBe(true);
    s().setOverlayState({ visible: false, pinned: false });
    expect(s()).toMatchObject({ visible: false, pinned: false });
  });
});

describe("settings store", () => {
  const s = () => useSettingsStore.getState();

  test("load, update, dirty, discard", () => {
    s().load(settings());
    expect(isDirty(s())).toBe(false);
    s().update((d) => ({ ...d, ui: { ...d.ui, theme: "light" } }));
    expect(isDirty(s())).toBe(true);
    s().discard();
    expect(s().draft?.ui.theme).toBe("dark");
  });

  test("external change replaces a clean draft", () => {
    s().load(settings());
    s().external(settings({ ui: { language: "de", theme: "dark", start_minimised: false, onboarding_done: false } }));
    expect(s().draft?.ui.language).toBe("de");
    expect(s().externalChange).toBe(false);
  });

  test("external change rebases unsaved edits onto untouched fields", () => {
    s().load(settings());
    s().update((d) => ({ ...d, ui: { ...d.ui, theme: "light" } }));
    s().external(settings({ ui: { language: "de", theme: "dark", start_minimised: false, onboarding_done: false } }));
    expect(s().draft?.ui).toEqual({ language: "de", theme: "light", start_minimised: false, onboarding_done: false });
    expect(s().base?.ui.language).toBe("de");
    expect(s().externalChange).toBe(false);
  });

  test("external change to a field the user edited flags a conflict", () => {
    s().load(settings());
    s().update((d) => ({ ...d, ui: { ...d.ui, theme: "light" } }));
    s().external(settings({ ui: { language: "en", theme: "system", start_minimised: false, onboarding_done: false } }));
    expect(s().draft?.ui.theme).toBe("light");
    expect(s().externalChange).toBe(true);
    s().discard();
    expect(s().draft?.ui.theme).toBe("system");
    expect(s().externalChange).toBe(false);
  });

  test("persisted changes update base and draft without dirtying", () => {
    s().load(settings());
    s().update((d) => ({ ...d, ui: { ...d.ui, theme: "light" } }));
    const chord = { modifiers: ["alt" as const], key: "KeyQ" };
    s().persisted((x) => ({ ...x, hotkeys: { ...x.hotkeys, price_check: chord } }));
    expect(s().base?.hotkeys.price_check).toEqual(chord);
    expect(s().draft?.hotkeys.price_check).toEqual(chord);
    s().discard();
    expect(isDirty(s())).toBe(false);
  });

  test("update before load is a no-op", () => {
    s().update((d) => d);
    s().persisted((d) => d);
    expect(s().draft).toBeNull();
  });
});
