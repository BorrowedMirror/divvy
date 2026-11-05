import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { emit } from "@tauri-apps/api/event";
import { mockIPC } from "@tauri-apps/api/mocks";
import { act } from "react";
import { AppProviders } from "../App";
import { createQueryClient } from "../hooks/queries";
import type { Chord, HotkeyAction, Settings_Serialize } from "../bindings";
import { diagnostics, leagues, settings } from "./fixtures";

export type Call = { cmd: string; args: Record<string, unknown> | undefined };
type Handler = (args: Record<string, unknown> | undefined) => unknown;

/** Marks a mocked command as failing with a Rust-side `IpcError`. */
export function fail(error: unknown): Promise<never> {
  return Promise.reject(error);
}

const defaults: Record<string, Handler> = {
  get_diagnostics: () => diagnostics(),
  list_leagues: () => leagues,
  hide_overlay: () => null,
  set_overlay_pinned: () => null,
  open_trade: () => null,
  open_settings: () => null,
};

/** Install `mockIPC` with event mocking; returns the recorded command calls. */
export function setupIpc(handlers: Record<string, Handler> = {}): Call[] {
  const calls: Call[] = [];
  // Like the Rust side, save_settings and rebind_hotkey persist and emit `settings-changed`.
  let current = settings();
  const all: Record<string, Handler> = {
    get_settings: () => current,
    save_settings: async (a) => {
      current = a?.settings as Settings_Serialize;
      await emit("settings-changed", { settings: current });
      return current;
    },
    rebind_hotkey: async (a) => {
      const action = a?.action as HotkeyAction;
      current = { ...current, hotkeys: { ...current.hotkeys, [action]: a?.chord as Chord } };
      await emit("settings-changed", { settings: current });
      return null;
    },
    ...defaults,
    ...handlers,
  };
  mockIPC(
    (cmd, args) => {
      const a = args as Record<string, unknown> | undefined;
      calls.push({ cmd, args: a });
      const h = all[cmd];
      if (!h) return fail({ kind: "internal", message: `unmocked ${cmd}`, retry_after_ms: null });
      return h(a);
    },
    { shouldMockEvents: true },
  );
  return calls;
}

export function callsOf(calls: Call[], cmd: string): Call[] {
  return calls.filter((c) => c.cmd === cmd);
}

export function renderWithProviders(ui: ReactElement) {
  const client = createQueryClient();
  return { client, ...render(<AppProviders client={client}>{ui}</AppProviders>) };
}

/** Emit a backend event through the mocked event plugin. */
export async function emitEvent(name: string, payload: unknown) {
  await act(async () => {
    await emit(name, payload);
  });
}

/** Let pending listen() registrations and promises settle. */
export async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}
