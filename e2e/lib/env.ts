// Paths and knobs shared by wdio.conf.ts and the specs. Every knob is an env var so the
// run-*.sh / run-windows.ps1 scripts and CI can override it.
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const exe = process.platform === "win32" ? ".exe" : "";

export const E2E_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const REPO_DIR = resolve(E2E_DIR, "..");
const target = process.env.CARGO_TARGET_DIR ?? join(REPO_DIR, "target");

/** The kalandra binary, built with `cargo build -p kalandra-desktop --features test-hook`. */
export const APP_BIN = process.env.KALANDRA_APP_BIN ?? join(target, "debug", `kalandra${exe}`);
export const FAKE_GAME_BIN = process.env.KALANDRA_FAKE_GAME_BIN ?? join(target, "debug", `fake-game${exe}`);
export const TAURI_DRIVER =
  process.env.TAURI_DRIVER ??
  (existsSync(join(homedir(), ".cargo", "bin", `tauri-driver${exe}`))
    ? join(homedir(), ".cargo", "bin", `tauri-driver${exe}`)
    : `tauri-driver${exe}`);
export const DRIVER_PORT = Number(process.env.KALANDRA_E2E_DRIVER_PORT ?? 4444);

/** Port of the debug test hook (`KALANDRA_TEST_HOOK`). */
export const HOOK_PORT = Number(process.env.KALANDRA_TEST_HOOK ?? 47123);
/** Port of the mock trade2 server; the app gets `KALANDRA_TRADE_BASE_URL=http://127.0.0.1:<port>`. */
export const MOCK_PORT = Number(process.env.KALANDRA_E2E_MOCK_PORT ?? 18080);
export const MOCK_URL = `http://127.0.0.1:${MOCK_PORT}`;

/** Item text the fake game serves; a rare ring routes through trade2 search/fetch. */
export const ITEM_FILE = process.env.KALANDRA_E2E_ITEM ?? join(E2E_DIR, "fixtures", "rare-ring.txt");
/**
 * A second rare ring with different rolls. The app caches trade2 searches by query for minutes,
 * so a re-check of the same item never reaches the mock; a different item forces a fresh query.
 */
export const ITEM_FILE_2 = process.env.KALANDRA_E2E_ITEM_2 ?? join(E2E_DIR, "fixtures", "rare-ring-2.txt");

/**
 * How the price-check hotkey is pressed:
 * - `xdotool`: real XTest key press of the global hotkey (X11 only; exercises the grab).
 * - `hook`: `trigger <pinned>` over the test-hook socket (Wayland, macOS, Windows CI).
 * - `auto` (default): `xdotool` when DISPLAY is set and WAYLAND_DISPLAY is not, else `hook`.
 */
export function triggerMode(): "xdotool" | "hook" {
  const m = process.env.KALANDRA_E2E_TRIGGER ?? "auto";
  if (m === "xdotool" || m === "hook") return m;
  return process.platform === "linux" && process.env.DISPLAY && !process.env.WAYLAND_DISPLAY ? "xdotool" : "hook";
}

/** xdotool chords for the default hotkeys (`Ctrl+D`, `Ctrl+Alt+D` in kalandra-config). */
export const HOTKEY = process.env.KALANDRA_E2E_HOTKEY ?? "ctrl+d";
export const HOTKEY_PINNED = process.env.KALANDRA_E2E_HOTKEY_PINNED ?? "ctrl+alt+d";
