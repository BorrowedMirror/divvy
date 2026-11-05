// Tray icon menu. Real tray hosts are not scriptable in CI, so menu items are activated
// through the test hook (`tray <menu-id>`), which dispatches the same handler as a click.
import { expect } from "@wdio/globals";
import { hookCommand, hookOk, hookState, waitForHook, waitForHookGone, waitForState } from "../lib/hook.ts";

describe("tray", () => {
  before(async () => {
    await waitForHook();
  });

  it("installs a tray icon", async () => {
    const s = await hookState();
    expect(s.tray_present).toBe(true);
  });

  it("rejects unknown menu ids", async () => {
    expect(await hookCommand("tray does-not-exist")).toMatch(/^err /);
  });

  it("'Settings' opens the settings window", async () => {
    await hookOk("tray open_settings");
    await waitForState((s) => s.settings_visible === true, 10000, "settings window visible");
  });

  // Must stay last: the app is gone afterwards.
  it("'Quit' exits the app", async () => {
    await hookOk("tray quit");
    await waitForHookGone(10000);
  });
});
