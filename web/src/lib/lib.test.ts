import { describe, expect, test } from "vitest";
import { filterSet, settings } from "../test/fixtures";
import { captureChord, formatChord, isFunctionKey, keyLabel, sameChord } from "./chord";
import { ageOf, formatAmount, formatPrice } from "./format";
import { IpcFailure, toIpcError, unwrap } from "./ipc";
import { rewiden, rewidenStat, widenValue } from "./rewiden";
import { validateSettings } from "./validate";

const key = (code: string, mods: Partial<Record<"ctrlKey" | "altKey" | "shiftKey" | "metaKey", boolean>> = {}) => ({
  code,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  metaKey: false,
  ...mods,
});

describe("rewiden", () => {
  test("integer values floor after widening", () => {
    expect(widenValue([100], 10)).toBe(90);
    expect(widenValue([85], 10)).toBe(76); // 76.5 -> 76
    expect(widenValue([85], 0)).toBe(85);
  });

  test("averages multi-value stats and ignores nulls", () => {
    expect(widenValue([10, null, 20], 10)).toBe(13); // mean 15 -> 13.5 -> 13
  });

  test("fractional values keep two decimals", () => {
    expect(widenValue([1.55], 10)).toBe(1.39);
  });

  test("negative values widen away from zero", () => {
    expect(widenValue([-20], 10)).toBe(-22);
  });

  test("clamps pct and handles empty values", () => {
    expect(widenValue([50], 150)).toBe(0);
    expect(widenValue([50], -5)).toBe(50);
    expect(widenValue([], 10)).toBeNull();
    expect(widenValue([null], 10)).toBeNull();
  });

  test("rewiden updates every stat min and widen_pct, keeps max/enabled", () => {
    const f = filterSet();
    const out = rewiden(f, 20);
    expect(out.widen_pct).toBe(20);
    expect(out.stats.map((s) => s.min)).toEqual([68, 25]);
    expect(out.stats.map((s) => s.enabled)).toEqual([true, false]);
    expect(out.stats[0]?.max).toBeNull();
    expect(f.stats[0]?.min).toBe(76); // pure
  });

  test("rewidenStat leaves stats without values alone", () => {
    const s = { stat_id: "x", text: "x", enabled: true, min: 3, max: null, item_values: [] };
    expect(rewidenStat(s, 10)).toBe(s);
  });
});

describe("chord capture", () => {
  test("modifier-only presses are pending", () => {
    expect(captureChord(key("ControlLeft", { ctrlKey: true }))).toEqual({ kind: "pending" });
    expect(captureChord(key(""))).toEqual({ kind: "pending" });
  });

  test("builds chord with W3C code and ordered modifiers", () => {
    expect(captureChord(key("KeyD", { ctrlKey: true, shiftKey: true }))).toEqual({
      kind: "chord",
      chord: { modifiers: ["ctrl", "shift"], key: "KeyD" },
    });
    expect(captureChord(key("KeyA", { altKey: true, metaKey: true }))).toEqual({
      kind: "chord",
      chord: { modifiers: ["alt", "meta"], key: "KeyA" },
    });
  });

  test("bare Escape cancels, bare letters need a modifier, F-keys are fine alone", () => {
    expect(captureChord(key("Escape"))).toEqual({ kind: "cancel" });
    expect(captureChord(key("KeyD"))).toEqual({ kind: "needs_modifier", key: "KeyD" });
    expect(captureChord(key("F5"))).toEqual({ kind: "chord", chord: { modifiers: [], key: "F5" } });
  });

  test("function key detection", () => {
    expect(isFunctionKey("F1")).toBe(true);
    expect(isFunctionKey("F24")).toBe(true);
    expect(isFunctionKey("F25")).toBe(false);
    expect(isFunctionKey("KeyF")).toBe(false);
  });

  test("formatting", () => {
    expect(formatChord({ modifiers: ["shift", "ctrl"], key: "KeyD" })).toBe("Ctrl+Shift+D");
    expect(formatChord({ modifiers: ["meta"], key: "Digit1" })).toBe("Meta+1");
    expect(keyLabel("Numpad5")).toBe("Num5");
    expect(keyLabel("ArrowUp")).toBe("Up");
    expect(keyLabel("F10")).toBe("F10");
  });

  test("sameChord ignores modifier order", () => {
    expect(sameChord({ modifiers: ["ctrl", "alt"], key: "KeyD" }, { modifiers: ["alt", "ctrl"], key: "KeyD" })).toBe(true);
    expect(sameChord({ modifiers: ["ctrl"], key: "KeyD" }, { modifiers: ["ctrl", "alt"], key: "KeyD" })).toBe(false);
    expect(sameChord({ modifiers: ["ctrl"], key: "KeyD" }, { modifiers: ["ctrl"], key: "KeyE" })).toBe(false);
    expect(sameChord({ modifiers: ["ctrl"], key: "KeyD" }, { modifiers: ["alt"], key: "KeyD" })).toBe(false);
  });
});

describe("format", () => {
  test("amounts and prices", () => {
    expect(formatAmount(null, "en")).toBe("?");
    expect(formatAmount(2.756, "en")).toBe("2.76");
    expect(formatAmount(1234.56, "en")).toBe("1,234.6");
    expect(formatPrice(null, "en", (c) => c)).toBe("—");
    expect(formatPrice({ amount: 3, currency: "divine" }, "en", () => "div")).toBe("3 div");
  });

  test("ages", () => {
    const now = Date.parse("2026-11-05T12:00:00Z");
    expect(ageOf(null, now)).toBeNull();
    expect(ageOf("garbage", now)).toBeNull();
    expect(ageOf("2026-11-05T11:59:40Z", now)).toEqual({ unit: "now", count: 0 });
    expect(ageOf("2026-11-05T11:15:00Z", now)).toEqual({ unit: "minutes", count: 45 });
    expect(ageOf("2026-11-05T07:00:00Z", now)).toEqual({ unit: "hours", count: 5 });
    expect(ageOf("2026-11-02T12:00:00Z", now)).toEqual({ unit: "days", count: 3 });
  });
});

describe("ipc helpers", () => {
  test("unwrap returns data or throws IpcFailure", () => {
    expect(unwrap({ status: "ok", data: 5 })).toBe(5);
    const err = { kind: "rate_limited", message: "slow down", retry_after_ms: 3000 };
    expect(() => unwrap({ status: "error", error: err })).toThrow(IpcFailure);
    try {
      unwrap({ status: "error", error: err });
    } catch (e) {
      expect(toIpcError(e)).toEqual(err);
    }
  });

  test("toIpcError normalises anything", () => {
    expect(toIpcError(new Error("x"))).toEqual({ kind: "internal", message: "x", retry_after_ms: null });
    expect(toIpcError({ kind: "trade", message: "y" })).toEqual({ kind: "trade", message: "y", retry_after_ms: null });
    expect(toIpcError("z")).toEqual({ kind: "internal", message: "z", retry_after_ms: null });
  });
});

describe("validateSettings", () => {
  test("defaults are valid", () => {
    expect(validateSettings(settings())).toEqual({});
  });

  test("flags every invalid field", () => {
    const s = settings();
    const bad = {
      ...s,
      league: { auto_detect: false, league: null },
      trade: { ...s.trade, widen_pct: 150, contact: "not-an-email" },
      overlay: { ...s.overlay, auto_hide_ms: -1, opacity: 0.1 },
      hotkeys: { ...s.hotkeys, open_settings: { modifiers: ["ctrl" as const], key: "KeyD" } },
    };
    expect(validateSettings(bad)).toEqual({
      "trade.widen_pct": "validation.widen",
      "trade.contact": "validation.email",
      "league.league": "validation.league",
      "overlay.auto_hide_ms": "validation.autoHide",
      "overlay.opacity": "validation.opacity",
      "hotkeys.open_settings": "validation.hotkeyClash",
    });
  });

  test("null widen / opacity and valid email are accepted", () => {
    const s = settings();
    expect(
      validateSettings({ ...s, trade: { ...s.trade, widen_pct: null, contact: "a@b.co" }, overlay: { ...s.overlay, opacity: 0.8 } }),
    ).toEqual({});
  });
});
