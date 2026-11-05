// Helpers that drive the app: trigger the hotkey, call IPC, pick windows.
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { browser } from "@wdio/globals";
import { HOTKEY, HOTKEY_PINNED, triggerMode } from "./env.ts";
import { hookOk } from "./hook.ts";

const run = promisify(execFile);

/** Focus the fake game window (X11). */
export async function focusGame(): Promise<void> {
  const { stdout } = await run("xdotool", ["search", "--name", "^Path of Exile 2$"]);
  const wid = stdout.trim().split("\n")[0];
  if (!wid) throw new Error("xdotool: no window named 'Path of Exile 2'");
  await run("xdotool", ["windowactivate", "--sync", wid]);
}

/** Press the price-check hotkey the most realistic way this environment allows. */
export async function triggerPriceCheck(pinned = false): Promise<void> {
  if (triggerMode() === "xdotool") {
    await focusGame();
    await run("xdotool", ["key", "--clearmodifiers", pinned ? HOTKEY_PINNED : HOTKEY]);
  } else {
    await hookOk(`trigger ${pinned ? 1 : 0}`);
  }
}

/**
 * Call a Tauri command from inside the current webview. Uses the internal invoke because
 * the generated bindings are bundled into the app, not importable from the test process.
 */
export async function invoke<T>(cmd: string, args: Record<string, unknown> = {}): Promise<T> {
  const r = await browser.execute(
    async (c: string, a: Record<string, unknown>) => {
      const w = window as unknown as {
        __TAURI_INTERNALS__?: { invoke: (c: string, a: Record<string, unknown>) => Promise<unknown> };
      };
      if (!w.__TAURI_INTERNALS__) return { err: "window.__TAURI_INTERNALS__ missing (not a Tauri webview?)" };
      try {
        return { ok: await w.__TAURI_INTERNALS__.invoke(c, a) };
      } catch (e) {
        return { err: JSON.stringify(e) };
      }
    },
    cmd,
    args,
  );
  const res = r as { ok?: unknown; err?: string };
  if (res.err !== undefined) throw new Error(`invoke ${cmd} failed: ${res.err}`);
  return res.ok as T;
}

/** Switch the webdriver context to the window whose URL hash is `#/<label>`. */
export async function switchToWindow(label: "overlay" | "settings" | "diagnostics", timeoutMs = 10000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  const seen: string[] = [];
  while (Date.now() < deadline) {
    for (const h of await browser.getWindowHandles()) {
      await browser.switchToWindow(h);
      const url = await browser.getUrl();
      seen.push(url);
      if (url.includes(`#/${label}`)) return;
    }
    await browser.pause(250);
  }
  throw new Error(`no webview with #/${label}; saw: ${[...new Set(seen)].join(", ")}`);
}
