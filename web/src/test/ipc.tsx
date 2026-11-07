import type { ReactElement } from "react";
import { act } from "react";

import { render } from "@testing-library/react";

import { createQueryClient } from "@hooks/queries";
import { mockTransport } from "@lib/mockTransport";
import { AppProviders } from "@src/App";
import type { Chord, HotkeyAction, Settings_Serialize } from "@src/bindings";
import { setTransport } from "@src/bindings";
import { diagnostics, leagues, settings } from "@test/fixtures";

export type Call = { cmd: string; args: Record<string, unknown> | undefined };
type Handler = (args: Record<string, unknown> | undefined) => unknown;

/** Marks a mocked command as failing with a backend `IpcError`. */
export function fail(error: unknown): Promise<never> {
  return Promise.reject(error);
}

const defaults: Record<string, Handler> = {
  get_diagnostics: () => diagnostics(),
  list_leagues: () => leagues,
  hide_overlay: () => null,
  get_overlay_layout: () => ({ cover: false, x: 0, y: 0 }),
  currency_icons: () => [],
  resize_overlay: () => null,
  set_overlay_pinned: () => null,
  get_overlay_snapshot: () => null,
  open_trade: () => null,
  open_settings: () => null,
  find_game_log: () => null,
};

let emit: (event: string, payload: unknown) => Promise<void> = async () => {};

/** Install an in-memory backend with events; returns the recorded command calls. */
export function setupIpc(handlers: Record<string, Handler> = {}): Call[] {
  const calls: Call[] = [];
  // Like the backend, save_settings and rebind_hotkey persist and emit `settings-changed`.
  let current = settings();
  const all: Record<string, Handler> = {
    get_settings: () => current,
    save_settings: async (a) => {
      current = a?.settings as Settings_Serialize;
      await emit("settings-changed", { settings: current });
      return current;
    },
    rebind_hotkey: async (a) => {
      const action = a?.action as Extract<HotkeyAction, string>;
      current = { ...current, hotkeys: { ...current.hotkeys, [action]: a?.chord as Chord } };
      await emit("settings-changed", { settings: current });
      return null;
    },
    ...defaults,
    ...handlers,
  };
  const mock = mockTransport((cmd, args) => {
    const a = Object.keys(args).length === 0 ? undefined : args;
    calls.push({ cmd, args: a });
    const h = all[cmd];
    if (!h) return fail({ kind: "internal", message: `unmocked ${cmd}`, retry_after_ms: null });
    return h(a);
  });
  emit = mock.emit;
  setTransport(mock.transport);
  return calls;
}

export function callsOf(calls: Call[], cmd: string): Call[] {
  return calls.filter((c) => c.cmd === cmd);
}

export function renderWithProviders(ui: ReactElement) {
  const client = createQueryClient();
  return { client, ...render(<AppProviders client={client}>{ui}</AppProviders>) };
}

/** Emit a backend event through the in-memory backend. */
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
