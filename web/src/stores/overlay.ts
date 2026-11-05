import { create } from "zustand";
import type {
  FilterSet_Serialize,
  IpcError_Serialize,
  ItemCopied_Serialize,
  MiscFilters_Serialize,
  OnlineStatus,
  ParsedItem_Serialize,
  PriceSummary_Serialize,
  RangeFilter_Serialize,
  StatFilter_Serialize,
} from "../bindings";
import { rewiden } from "../lib/rewiden";

export type Phase = "idle" | "loading" | "ready" | "error";

export type RangeKey = Exclude<keyof MiscFilters_Serialize, "corrupted" | "identified">;

export type OverlayStore = {
  item: ParsedItem_Serialize | null;
  filters: FilterSet_Serialize | null;
  summary: PriceSummary_Serialize | null;
  error: IpcError_Serialize | null;
  /** Epoch ms after which a rate-limited query may be retried. */
  retryAt: number | null;
  phase: Phase;
  pinned: boolean;
  visible: boolean;
  itemCopied: (payload: ItemCopied_Serialize) => void;
  updateStat: (index: number, patch: Partial<StatFilter_Serialize>) => void;
  updateRange: (key: RangeKey, patch: Partial<RangeFilter_Serialize>) => void;
  setCorrupted: (v: boolean | null) => void;
  setStatus: (status: OnlineStatus) => void;
  setLeague: (league: string) => void;
  widen: (pct: number) => void;
  queryStarted: () => void;
  priceReady: (summary: PriceSummary_Serialize) => void;
  priceFailed: (error: IpcError_Serialize, now?: number) => void;
  setPinned: (pinned: boolean) => void;
  setOverlayState: (s: { visible: boolean; pinned: boolean }) => void;
  reset: () => void;
};

const initial: Pick<
  OverlayStore,
  "item" | "filters" | "summary" | "error" | "retryAt" | "phase" | "pinned" | "visible"
> = {
  item: null,
  filters: null,
  summary: null,
  error: null,
  retryAt: null,
  phase: "idle",
  pinned: false,
  visible: true,
};

export const useOverlayStore = create<OverlayStore>()((set) => {
  const patchFilters = (fn: (f: FilterSet_Serialize) => FilterSet_Serialize) =>
    set((s) => (s.filters ? { filters: fn(s.filters) } : {}));

  return {
    ...initial,
    itemCopied: ({ item, filters, pinned }) =>
      set({ item, filters, pinned, summary: null, error: null, retryAt: null, phase: "idle" }),
    updateStat: (index, patch) =>
      patchFilters((f) => ({ ...f, stats: f.stats.map((s, i) => (i === index ? { ...s, ...patch } : s)) })),
    updateRange: (key, patch) =>
      patchFilters((f) => ({ ...f, misc: { ...f.misc, [key]: { ...f.misc[key], ...patch } } })),
    setCorrupted: (corrupted) => patchFilters((f) => ({ ...f, misc: { ...f.misc, corrupted } })),
    setStatus: (status) => patchFilters((f) => ({ ...f, status })),
    setLeague: (league) => patchFilters((f) => ({ ...f, league })),
    widen: (pct) => patchFilters((f) => rewiden(f, pct)),
    queryStarted: () => set({ phase: "loading", error: null }),
    priceReady: (summary) => set({ summary, phase: "ready", error: null, retryAt: null }),
    priceFailed: (error, now = Date.now()) =>
      set({
        error,
        phase: "error",
        retryAt: error.kind === "rate_limited" && error.retry_after_ms ? now + error.retry_after_ms : null,
      }),
    setPinned: (pinned) => set({ pinned }),
    setOverlayState: ({ visible, pinned }) => set({ visible, pinned }),
    reset: () => set(initial),
  };
});
