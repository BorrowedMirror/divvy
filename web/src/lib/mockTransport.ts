import type { Transport } from "@src/bindings";

export type MockHandler = (cmd: string, args: Record<string, unknown>) => unknown;

/**
 * An in-memory backend for tests and the browser preview: commands go to `handler` by their
 * command name (`get_settings`, ...), and `emit` delivers an event to every listener, the way the
 * Go backend's events arrive in the app. A handler that rejects with an `IpcError` object makes
 * the command resolve to `{status: "error"}`.
 */
export function mockTransport(handler: MockHandler) {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  const transport: Transport = {
    invoke: async (cmd, args) => handler(cmd, args),
    listen: async (event, cb) => {
      let set = listeners.get(event);
      if (!set) {
        set = new Set();
        listeners.set(event, set);
      }
      set.add(cb);
      return () => {
        set.delete(cb);
      };
    },
  };
  const emit = async (event: string, payload: unknown) => {
    for (const cb of [...(listeners.get(event) ?? [])]) cb(payload);
  };
  return { transport, emit };
}

/** True inside the app's web view (WebView2 on Windows, WebKit on Linux and macOS). */
export function inNativeWebview(): boolean {
  const w = window as unknown as {
    chrome?: { webview?: unknown };
    webkit?: { messageHandlers?: unknown };
  };
  return w.chrome?.webview !== undefined || w.webkit?.messageHandlers !== undefined;
}
