import { useEffect, useRef } from "react";
import { commands } from "../bindings";

/**
 * Keep the (Tauri) window exactly as tall as `ref`'s content: measures it with a
 * ResizeObserver and asks Rust to resize the window (`resize_overlay`), which clamps to the
 * monitor. Only content taller than the screen ends up scrolling.
 */
export function useFitWindowHeight<T extends HTMLElement>() {
  const ref = useRef<T | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let last = 0;
    let frame = 0;
    let scheduled = false;
    const report = () => {
      scheduled = false;
      const height = Math.ceil(el.getBoundingClientRect().height);
      if (height > 0 && Math.abs(height - last) >= 1) {
        last = height;
        void commands.resizeOverlay(height);
      }
    };
    const observer = new ResizeObserver(() => {
      if (scheduled) return;
      scheduled = true;
      frame = requestAnimationFrame(report);
    });
    observer.observe(el);
    report();
    return () => {
      observer.disconnect();
      if (scheduled) cancelAnimationFrame(frame);
    };
  }, []);
  return ref;
}
