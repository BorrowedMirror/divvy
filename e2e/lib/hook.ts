// Client for the desktop app's debug test hook (feature `test-hook`, env KALANDRA_TEST_HOOK).
// Line protocol over localhost TCP; each command gets exactly one reply line:
//   trigger <0|1>        -> ok | err <msg>
//   state                -> {"overlay_visible":bool,"last_price":PriceSummary|null, ...}
//   tray <menu-id>       -> ok | err <msg>     (ids: open_settings, quit)
//   quit                 -> ok (then the app exits)
import { connect } from "node:net";
import { HOOK_PORT } from "./env.ts";

export interface Price {
  amount: number;
  currency: string;
}

export interface HookState {
  overlay_visible: boolean;
  last_price: { total: number; median: Price | null; min: Price | null; max: Price | null } | null;
  /** Additive fields (see CONTRACTS.md "Contract changes"). */
  overlay_pinned?: boolean;
  settings_visible?: boolean;
  tray_present?: boolean;
}

export function hookCommand(cmd: string, timeoutMs = 5000, port = HOOK_PORT): Promise<string> {
  return new Promise((resolve, reject) => {
    const sock = connect({ host: "127.0.0.1", port });
    let buf = "";
    const timer = setTimeout(() => {
      sock.destroy();
      reject(new Error(`test hook: no reply to "${cmd}" within ${timeoutMs}ms`));
    }, timeoutMs);
    sock.setEncoding("utf8");
    sock.on("connect", () => sock.write(`${cmd}\n`));
    sock.on("data", (d: string) => {
      buf += d;
      const nl = buf.indexOf("\n");
      if (nl >= 0) {
        clearTimeout(timer);
        sock.end();
        resolve(buf.slice(0, nl).trim());
      }
    });
    sock.on("error", (e) => {
      clearTimeout(timer);
      reject(
        new Error(
          `test hook unreachable on 127.0.0.1:${port} (${e.message}). Is the app built with ` +
            "`cargo build -p kalandra-desktop --features test-hook` and started with KALANDRA_TEST_HOOK?",
        ),
      );
    });
    sock.on("end", () => {
      if (!buf.includes("\n")) {
        clearTimeout(timer);
        if (buf) resolve(buf.trim());
        else reject(new Error(`test hook closed without replying to "${cmd}"`));
      }
    });
  });
}

export async function hookOk(cmd: string): Promise<void> {
  const r = await hookCommand(cmd);
  if (r !== "ok") throw new Error(`test hook "${cmd}" replied: ${r}`);
}

export async function hookState(): Promise<HookState> {
  const r = await hookCommand("state");
  try {
    return JSON.parse(r) as HookState;
  } catch {
    throw new Error(`test hook "state" did not return JSON: ${r}`);
  }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Wait until the hook accepts connections (the app has finished starting). */
export async function waitForHook(timeoutMs = 30000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let last: unknown;
  while (Date.now() < deadline) {
    try {
      await hookState();
      return;
    } catch (e) {
      last = e;
      await sleep(250);
    }
  }
  throw new Error(`test hook never came up: ${String(last)}`);
}

/** Poll `state` until `pred` holds; the error includes the last state seen. */
export async function waitForState(pred: (s: HookState) => boolean, timeoutMs = 20000, what = "state"): Promise<HookState> {
  const deadline = Date.now() + timeoutMs;
  let last: HookState | undefined;
  while (Date.now() < deadline) {
    last = await hookState();
    if (pred(last)) return last;
    await sleep(200);
  }
  throw new Error(`timed out waiting for ${what}; last state: ${JSON.stringify(last)}`);
}

/** Resolve once the hook stops answering (app exited). */
export async function waitForHookGone(timeoutMs = 10000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      await hookCommand("state", 1000);
    } catch {
      return;
    }
    await sleep(200);
  }
  throw new Error("app still answering on the test hook after quit");
}
