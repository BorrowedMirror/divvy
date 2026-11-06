import { create } from "zustand";
import type { Settings_Serialize } from "@src/bindings";

export type SettingsStore = {
  /** Last settings known to be persisted by the backend. */
  base: Settings_Serialize | null;
  /** Working copy edited by the settings window. */
  draft: Settings_Serialize | null;
  /** Backend settings changed a field the user is also editing. */
  externalChange: boolean;
  load: (s: Settings_Serialize) => void;
  /** `settings-changed` from elsewhere: rebase unsaved edits onto it (see `rebase`). */
  external: (s: Settings_Serialize) => void;
  update: (fn: (draft: Settings_Serialize) => Settings_Serialize) => void;
  /** Apply a change the backend already persisted (e.g. a hotkey rebind) to base and draft. */
  persisted: (fn: (s: Settings_Serialize) => Settings_Serialize) => void;
  discard: () => void;
};

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

export function isDirty(s: Pick<SettingsStore, "base" | "draft">): boolean {
  return !same(s.base, s.draft);
}

type Section = Exclude<keyof Settings_Serialize, "version">;
const SECTIONS: Section[] = ["league", "hotkeys", "trade", "overlay", "ui", "platform"];

/**
 * Three-way merge per field: fields the user hasn't touched (draft == base) take the incoming
 * value; edited fields keep the draft. `conflict` is true when an edited field also changed
 * externally to something different.
 */
export function rebase(
  base: Settings_Serialize,
  draft: Settings_Serialize,
  incoming: Settings_Serialize,
): { draft: Settings_Serialize; conflict: boolean } {
  let conflict = false;
  const out = { ...incoming };
  for (const sec of SECTIONS) {
    const b = base[sec] as Record<string, unknown>;
    const d = draft[sec] as Record<string, unknown>;
    const inc = incoming[sec] as Record<string, unknown>;
    const merged: Record<string, unknown> = { ...inc };
    for (const k of Object.keys(d)) {
      if (same(d[k], b[k])) continue;
      merged[k] = d[k];
      if (!same(inc[k], b[k]) && !same(inc[k], d[k])) conflict = true;
    }
    (out as Record<string, unknown>)[sec] = merged;
  }
  return { draft: out, conflict };
}

export const useSettingsStore = create<SettingsStore>()((set) => ({
  base: null,
  draft: null,
  externalChange: false,
  load: (s) => set({ base: s, draft: s, externalChange: false }),
  external: (s) =>
    set((st) => {
      if (st.base === null || st.draft === null || !isDirty(st)) return { base: s, draft: s, externalChange: false };
      const { draft, conflict } = rebase(st.base, st.draft, s);
      return { base: s, draft, externalChange: st.externalChange || conflict };
    }),
  update: (fn) => set((st) => (st.draft ? { draft: fn(st.draft) } : {})),
  persisted: (fn) =>
    set((st) => (st.base && st.draft ? { base: fn(st.base), draft: fn(st.draft) } : {})),
  discard: () => set((st) => ({ draft: st.base, externalChange: false })),
}));
