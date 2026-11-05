// WebdriverIO + tauri-driver config. Run through e2e/scripts/run-{x11,wayland}.sh or
// run-windows.ps1, which provide the display server and build the binaries.
//
// Missing prerequisites throw SevereServiceError, which aborts the whole run with that message.
//
// The launcher process owns the mock trade2 server (shared by all specs, reset per spec);
// each worker owns one tauri-driver and a fresh KALANDRA_CONFIG_DIR, which survives
// `browser.reloadSession()` so restart-persistence can be tested.
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { connect } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { APP_BIN, DRIVER_PORT, FAKE_GAME_BIN, HOOK_PORT, MOCK_PORT, MOCK_URL, TAURI_DRIVER } from "./lib/env.ts";
import { SevereServiceError } from "webdriverio";
import { TradeMockServer } from "./mock-trade/server.ts";

let mock: TradeMockServer | undefined;
let driver: ChildProcess | undefined;
let configDir: string | undefined;

function requireFile(path: string, hint: string): void {
  if (!existsSync(path)) throw new SevereServiceError(`e2e: ${path} does not exist. ${hint}`);
}

function nativeDriverArgs(): string[] {
  const explicit = process.env.KALANDRA_NATIVE_DRIVER;
  if (explicit) return ["--native-driver", explicit];
  if (process.platform !== "linux") return [];
  const found = spawnSync("sh", ["-c", "command -v WebKitWebDriver"], { encoding: "utf8" });
  if (found.status !== 0) {
    throw new SevereServiceError(
      "e2e: WebKitWebDriver not found in PATH (tauri-driver needs it on Linux). Install it " +
        "(Arch: webkitgtk-6.0 provides /usr/bin/WebKitWebDriver; Debian/Ubuntu: " +
        "webkit2gtk-driver) or set KALANDRA_NATIVE_DRIVER=/path/to/WebKitWebDriver.",
    );
  }
  return [];
}

async function waitPort(port: number, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const ok = await new Promise<boolean>((resolve) => {
      const s = connect({ host: "127.0.0.1", port });
      s.once("connect", () => (s.end(), resolve(true)));
      s.once("error", () => resolve(false));
    });
    if (ok) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`e2e: nothing listening on 127.0.0.1:${port} after ${timeoutMs}ms`);
}

export const config: WebdriverIO.Config = {
  runner: "local",
  specs: ["./specs/**/*.e2e.ts"],
  maxInstances: 1,
  hostname: "127.0.0.1",
  port: DRIVER_PORT,
  capabilities: [
    {
      // tauri-driver launches this binary; it inherits the worker's environment.
      "tauri:options": { application: APP_BIN },
    } as WebdriverIO.Capabilities,
  ],
  logLevel: "warn",
  waitforTimeout: 15000,
  connectionRetryTimeout: 60000,
  connectionRetryCount: 1,
  framework: "mocha",
  reporters: ["spec"],
  mochaOpts: { ui: "bdd", timeout: 120000 },

  onPrepare: async () => {
    requireFile(APP_BIN, "Build it with `cargo build -p kalandra-desktop --features test-hook,tauri/custom-protocol` (or set KALANDRA_APP_BIN).");
    requireFile(FAKE_GAME_BIN, "Build it with `cargo build -p fake-game` (or set KALANDRA_FAKE_GAME_BIN).");
    mock = new TradeMockServer();
    await mock.listen(MOCK_PORT);
    // Inherited by workers -> tauri-driver -> the app.
    process.env.KALANDRA_TRADE_BASE_URL = MOCK_URL;
    process.env.KALANDRA_TEST_HOOK = String(HOOK_PORT);
    process.env.RUST_LOG ??= "kalandra=debug,info";
  },

  onComplete: async () => {
    await mock?.close();
  },

  beforeSession: async () => {
    configDir = mkdtempSync(join(tmpdir(), "kalandra-e2e-config-"));
    process.env.KALANDRA_CONFIG_DIR = configDir;
    driver = spawn(TAURI_DRIVER, ["--port", String(DRIVER_PORT), ...nativeDriverArgs()], {
      stdio: ["ignore", "inherit", "inherit"],
    });
    driver.once("error", (e) => {
      throw new Error(`e2e: failed to start tauri-driver (${TAURI_DRIVER}): ${e.message}; cargo install tauri-driver`);
    });
    await waitPort(DRIVER_PORT, 15000);
  },

  afterSession: () => {
    driver?.kill();
    driver = undefined;
    if (configDir && !process.env.KALANDRA_E2E_KEEP_CONFIG) rmSync(configDir, { recursive: true, force: true });
  },
};
