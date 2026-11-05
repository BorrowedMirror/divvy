import { create } from "zustand";
import type { UpdateAvailable_Serialize } from "../bindings";

export type UpdateStore = {
  /** Latest `update-available` payload. */
  available: UpdateAvailable_Serialize | null;
  /** Version the user dismissed; a newer one shows the banner again. */
  dismissed: string | null;
  installing: boolean;
  announce: (u: UpdateAvailable_Serialize) => void;
  dismiss: () => void;
  setInstalling: (v: boolean) => void;
};

export const useUpdateStore = create<UpdateStore>((set) => ({
  available: null,
  dismissed: null,
  installing: false,
  announce: (available) => set({ available }),
  dismiss: () => set((s) => ({ dismissed: s.available?.version ?? null })),
  setInstalling: (installing) => set({ installing }),
}));
