import { create } from "zustand";
import type { Settings_Serialize } from "../bindings";

export type SettingsStore = {
  /** Last settings known to be persisted by the backend. */
  base: Settings_Serialize | null;
  /** Working copy edited by the settings window. */
  draft: Settings_Serialize | null;
  /** Backend settings changed while the draft had unsaved edits. */
  externalChange: boolean;
  load: (s: Settings_Serialize) => void;
  /** `settings-changed` from elsewhere: adopt it unless the user has unsaved edits. */
  external: (s: Settings_Serialize) => void;
  update: (fn: (draft: Settings_Serialize) => Settings_Serialize) => void;
  discard: () => void;
};

export function isDirty(s: Pick<SettingsStore, "base" | "draft">): boolean {
  return JSON.stringify(s.base) !== JSON.stringify(s.draft);
}

export const useSettingsStore = create<SettingsStore>()((set, get) => ({
  base: null,
  draft: null,
  externalChange: false,
  load: (s) => set({ base: s, draft: s, externalChange: false }),
  external: (s) => {
    if (get().draft === null || !isDirty(get())) set({ base: s, draft: s, externalChange: false });
    else set({ base: s, externalChange: true });
  },
  update: (fn) => set((st) => (st.draft ? { draft: fn(st.draft) } : {})),
  discard: () => set((st) => ({ draft: st.base, externalChange: false })),
}));
