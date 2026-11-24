import { expect, test, vi } from "vitest";

import { commands, events } from "@src/bindings";
import { installDevMock } from "@src/devMock";
import { diagnostics, filterSet, itemCopied, leagues, priceSummary, settings } from "@test/fixtures";

// The fixtures with the settings hotkey the app ships with, as the preview shows them.
function shipped() {
  const s = settings();
  return { ...s, hotkeys: { ...s.hotkeys, open_settings: { modifiers: ["ctrl" as const], key: "Space" } } };
}

test("dev mock answers every command from fixtures", async () => {
  installDevMock("settings");
  expect(await commands.getSettings()).toEqual(shipped());
  const edited = { ...shipped(), ui: { ...settings().ui, theme: "light" as const } };
  expect(await commands.saveSettings(edited)).toEqual({ status: "ok", data: edited });
  expect(await commands.getSettings()).toEqual(edited);
  expect(await commands.getDiagnostics()).toEqual(diagnostics());
  expect(await commands.listLeagues()).toEqual({ status: "ok", data: leagues });
  expect(await commands.hideOverlay()).toEqual({ status: "ok", data: null });
  expect((await commands.recheckPermissions()).platform).toBe("macos");
  expect((await commands.getPermissions()).missing).toHaveLength(3);
  expect(await commands.getOverlayLayout()).toEqual({ cover: false, x: 0, y: 0 });
  expect(await commands.getOverlaySnapshot()).toBeNull();
});

test("dev mock hands the overlay its item when it catches up", async () => {
  installDevMock("overlay");
  expect(await commands.getOverlaySnapshot()).toEqual(itemCopied());
});

test("dev mock resolves queries and emits an item on the overlay route", async () => {
  vi.useFakeTimers();
  try {
    installDevMock("overlay");
    const seen = vi.fn();
    await events.itemCopied.listen((e) => seen(e.payload));
    const pending = commands.queryPrice(filterSet());
    await vi.advanceTimersByTimeAsync(500);
    expect(await pending).toEqual({ status: "ok", data: priceSummary() });
    expect(seen).toHaveBeenCalledExactlyOnceWith(itemCopied());
  } finally {
    vi.useRealTimers();
  }
});

test("dev mock does not emit items outside the overlay", async () => {
  vi.useFakeTimers();
  try {
    installDevMock("diagnostics");
    const seen = vi.fn();
    await events.itemCopied.listen(seen);
    await vi.advanceTimersByTimeAsync(1000);
    expect(seen).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});
