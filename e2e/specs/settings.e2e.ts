// Settings are saved to KALANDRA_CONFIG_DIR and survive an app restart.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { $, browser, expect } from "@wdio/globals";
import { invoke, switchToWindow } from "../lib/app.ts";
import { waitForHook } from "../lib/hook.ts";

interface Settings {
  league: { auto_detect: boolean; league: string | null };
  trade: { widen_pct: number };
  [k: string]: unknown;
}

function findSettingsFile(dir: string): string | undefined {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) {
      const f = findSettingsFile(p);
      if (f) return f;
    } else if (name === "settings.toml") {
      return p;
    }
  }
  return undefined;
}

describe("settings", () => {
  const configDir = process.env.KALANDRA_CONFIG_DIR!;

  before(async () => {
    await waitForHook();
    await switchToWindow("settings");
  });

  it("uses the isolated config dir from KALANDRA_CONFIG_DIR", () => {
    expect(configDir).toBeTruthy();
  });

  it("saves settings to disk", async () => {
    const s = await invoke<Settings>("get_settings");
    s.league.auto_detect = false;
    s.league.league = "Hardcore";
    s.trade.widen_pct = 17;
    const saved = await invoke<Settings>("save_settings", { settings: s });
    expect(saved.league.league).toBe("Hardcore");

    const file = findSettingsFile(configDir);
    expect(file).toBeDefined();
    const toml = readFileSync(file!, "utf8");
    expect(toml).toContain("Hardcore");
    expect(toml).toMatch(/widen_pct\s*=\s*17/);
  });

  it("restores them after a restart", async () => {
    await browser.reloadSession(); // tauri-driver relaunches the app with the same env
    await waitForHook();
    await switchToWindow("settings");
    const s = await invoke<Settings>("get_settings");
    expect(s.league.auto_detect).toBe(false);
    expect(s.league.league).toBe("Hardcore");
    expect(s.trade.widen_pct).toBe(17);
  });

  it("renders the settings page", async () => {
    await invoke("open_settings");
    const root = await $('[data-testid="settings-root"]');
    await root.waitForDisplayed({ timeout: 10000 });
  });
});
