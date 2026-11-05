import { useCallback, useSyncExternalStore } from "react";

function subscribeClock(onTick: () => void): () => void {
  const id = setInterval(onTick, 250);
  return () => clearInterval(id);
}

/** Whole seconds remaining until `deadline` (epoch ms), re-rendering as it ticks; 0 when passed. */
export function useCountdown(deadline: number | null): number {
  const snapshot = useCallback(
    () => (deadline === null ? 0 : Math.max(0, Math.ceil((deadline - Date.now()) / 1000))),
    [deadline],
  );
  const subscribe = useCallback(
    (onTick: () => void) => (deadline === null ? () => {} : subscribeClock(onTick)),
    [deadline],
  );
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
