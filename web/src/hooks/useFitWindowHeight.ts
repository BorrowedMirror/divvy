import type { RefObject } from "react";
import { useEffect, useRef } from "react";

import { commands } from "@src/bindings";

/**
 * Keep the (Tauri) window exactly as tall as `ref`'s content: measures it with a
 * ResizeObserver and asks Rust to resize the window (`resize_overlay`), which clamps to the
 * monitor. Only content taller than the screen ends up scrolling.
 */
export function useFitWindowHeight<T extends HTMLElement>(
  content?: RefObject<HTMLElement | null>,
  extraHeight: () => number = () => 0,
) {
  const ref = useRef<T | null>(null);
  const extraRef = useRef(extraHeight);
  useEffect(() => {
    extraRef.current = extraHeight;
  });
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let last = 0;
    let lastWidth = 0;
    let frame = 0;
    let scheduled = false;
    const report = () => {
      scheduled = false;
      const rect = el.getBoundingClientRect();
      // With `content`, report the content's full height (the card may be capped and scroll).
      const inner = content?.current;
      const height = Math.ceil(inner ? inner.getBoundingClientRect().height + extraRef.current() : rect.height);
      const width = Math.ceil(rect.width);
      if (height > 0 && (Math.abs(height - last) >= 1 || Math.abs(width - lastWidth) >= 1)) {
        last = height;
        lastWidth = width;
        void commands.resizeOverlay(width, height);
      }
    };
    const observer = new ResizeObserver(() => {
      if (scheduled) return;
      scheduled = true;
      frame = requestAnimationFrame(report);
    });
    observer.observe(el);
    if (content?.current) observer.observe(content.current);
    report();
    return () => {
      observer.disconnect();
      if (scheduled) cancelAnimationFrame(frame);
    };
  }, [content]);
  return ref;
}
