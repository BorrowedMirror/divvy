import { Events } from "@wailsio/runtime";

import { inNativeWebview } from "@lib/mockTransport";
import { type Route, routeFromHash } from "@src/route";

/**
 * Read-only DOM checks for the end-to-end tests (`dom <window> <selector>` on the test hook).
 *
 * Test-hook builds of the Go app emit `e2e-query` {id, window, selector}; the web view showing
 * that window answers with `e2e-reply` {id, exists, visible, text} for the first element matching
 * the selector. Nothing else is read and nothing is evaluated. Release builds never emit
 * `e2e-query`, so there the listener stays idle.
 */
export const QUERY_EVENT = "e2e-query";
export const REPLY_EVENT = "e2e-reply";

export type DomQuery = { id: string; window: string; selector: string };
export type DomReply = { id: string; exists: boolean; visible: boolean; text: string };

/** The window a route is shown in: the overlay, or the settings window for every other page. */
export function windowOf(route: Route): "overlay" | "settings" {
  return route === "overlay" ? "overlay" : "settings";
}

function isQuery(v: unknown): v is DomQuery {
  const q = v as Partial<DomQuery> | null;
  return (
    typeof q === "object" &&
    q !== null &&
    typeof q.id === "string" &&
    typeof q.window === "string" &&
    typeof q.selector === "string"
  );
}

function isVisible(el: Element): boolean {
  if (typeof el.checkVisibility === "function") {
    return el.checkVisibility({ opacityProperty: true, visibilityProperty: true });
  }
  const style = getComputedStyle(el);
  return style.display !== "none" && style.visibility !== "hidden" && el.getClientRects().length > 0;
}

/** The reply to `query` from this document, or null when the query is for another window. */
export function answer(query: unknown, doc: Document, route: Route): DomReply | null {
  if (!isQuery(query) || query.window !== windowOf(route)) return null;
  let el: Element | null = null;
  try {
    el = doc.querySelector(query.selector);
  } catch {
    // An invalid selector matches nothing.
  }
  if (!el) return { id: query.id, exists: false, visible: false, text: "" };
  return { id: query.id, exists: true, visible: isVisible(el), text: el.textContent ?? "" };
}

type EventsApi = {
  On(name: string, cb: (e: { data: unknown }) => void): () => void;
  Emit(name: string, data?: unknown): unknown;
};

/** Answer `e2e-query` events in the app's web view; a no-op elsewhere. Returns the unsubscriber. */
export function installE2E(events: EventsApi = Events, native: boolean = inNativeWebview()): () => void {
  if (!native) return () => {};
  return events.On(QUERY_EVENT, (e) => {
    const reply = answer(e.data, document, routeFromHash(window.location.hash));
    if (reply) void events.Emit(REPLY_EVENT, reply);
  });
}
