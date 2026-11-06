import { useCallback, useEffect, useRef } from "react";

/**
 * Start a timer of `delayMs` when the pointer leaves the panel and cancel it when it comes back.
 * `0` disables auto-hide; `isPinned` is checked both when the timer starts and when it fires.
 */
export function useAutoHide(delayMs: number, isPinned: () => boolean, hide: () => void) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancel = useCallback(() => {
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    timerRef.current = null;
  }, []);
  const onMouseLeave = useCallback(() => {
    cancel();
    if (delayMs <= 0 || isPinned()) return;
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      if (!isPinned()) hide();
    }, delayMs);
  }, [cancel, delayMs, isPinned, hide]);
  useEffect(() => cancel, [cancel]);
  return { onMouseLeave, onMouseEnter: cancel };
}
