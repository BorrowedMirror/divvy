import { listen } from "@tauri-apps/api/event";
import { expect, test, vi } from "vitest";
import { commands } from "./bindings";
import { installDevMock } from "./devMock";

test("dev mock answers every command from fixtures", async () => {
  installDevMock("settings");
  const s = await commands.getSettings();
  expect(s.ui.language).toBe("en");
  const saved = await commands.saveSettings({ ...s, ui: { ...s.ui, theme: "light" } });
  expect(saved.status).toBe("ok");
  expect((await commands.getSettings()).ui.theme).toBe("light");
  expect((await commands.getDiagnostics()).version).toBeTruthy();
  expect((await commands.listLeagues()).status).toBe("ok");
  expect((await commands.hideOverlay()).status).toBe("ok");
});

test("dev mock resolves queries and emits an item on the overlay route", async () => {
  vi.useFakeTimers();
  try {
    installDevMock("overlay");
    const seen = vi.fn();
    await listen("item-copied", seen);
    const pending = commands.queryPrice({} as never);
    await vi.advanceTimersByTimeAsync(500);
    expect((await pending).status).toBe("ok");
    expect(seen).toHaveBeenCalledOnce();
  } finally {
    vi.useRealTimers();
  }
});
