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

/**
 * Wait until the (still hidden) overlay webview has loaded the app and rendered the overlay.
 *
 * The app emits `item-copied` once and the overlay does not ask for it again, so a hotkey that
 * fires before the overlay's React tree has subscribed is lost (no price query ever runs). Real
 * users never press the hotkey within a second of launch; the specs do, so they wait here.
 * Also fails fast on a binary built without `tauri/custom-protocol`, whose webviews point at the
 * Vite dev server (`http://localhost:5173`) and never load anything.
 */
export async function waitForOverlayReady(timeoutMs = 20000): Promise<void> {
  await switchToWindow("overlay", timeoutMs);
  const url = await browser.getUrl();
  if (url.startsWith("http://localhost:5173")) {
    throw new Error(
      `overlay webview loads ${url}: the app was built without \`tauri/custom-protocol\` ` +
        "(rebuild with `cargo build -p kalandra-desktop --features test-hook,tauri/custom-protocol`)",
    );
  }
  await browser.waitUntil(
    () =>
      browser.execute(
        () => document.readyState === "complete" && document.querySelector("article[aria-label]") !== null,
      ),
    { timeout: timeoutMs, interval: 100, timeoutMsg: `overlay app never rendered (${url})` },
  );
}
