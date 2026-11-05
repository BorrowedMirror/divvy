import { useEffect, useRef } from "react";

type Listenable<T> = {
  listen: (cb: (event: { payload: T }) => void) => Promise<() => void>;
};

/** Subscribe to a tauri-specta event for the lifetime of the component. */
export function useTauriEvent<T>(event: Listenable<T>, handler: (payload: T) => void): void {
  const ref = useRef(handler);
  useEffect(() => {
    ref.current = handler;
  });
  useEffect(() => {
    let unlisten: (() => void) | undefined;
    let disposed = false;
    event
      .listen((e) => ref.current(e.payload))
      .then((fn) => {
        if (disposed) fn();
        else unlisten = fn;
      })
      .catch(() => {
        // No Tauri runtime (plain browser preview): events simply never arrive.
      });
    return () => {
      disposed = true;
      unlisten?.();
    };
  }, [event]);
}
