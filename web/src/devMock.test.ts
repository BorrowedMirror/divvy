import { listen } from "@tauri-apps/api/event";
import { expect, test, vi } from "vitest";
import { commands } from "./bindings";
import { installDevMock } from "./devMock";
import { diagnostics, filterSet, itemCopied, leagues, priceSummary, settings } from "./test/fixtures";

test("dev mock answers every command from fixtures", async () => {
  installDevMock("settings");
  expect(await commands.getSettings()).toEqual(settings());
  const edited = { ...settings(), ui: { ...settings().ui, theme: "light" as const } };
  expect(await commands.saveSettings(edited)).toEqual({ status: "ok", data: edited });
  expect(await commands.getSettings()).toEqual(edited);
  expect(await commands.getDiagnostics()).toEqual(diagnostics());
  expect(await commands.listLeagues()).toEqual({ status: "ok", data: leagues });
  expect(await commands.hideOverlay()).toEqual({ status: "ok", data: null });
  expect((await commands.recheckPermissions()).platform).toBe("macos");
  expect((await commands.getPermissions()).missing).toHaveLength(3);
});

test("dev mock resolves queries and emits an item on the overlay route", async () => {
  vi.useFakeTimers();
  try {
    installDevMock("overlay");
    const seen = vi.fn();
    await listen("item-copied", (e) => seen(e.payload));
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
    await listen("item-copied", seen);
    await vi.advanceTimersByTimeAsync(1000);
    expect(seen).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});
