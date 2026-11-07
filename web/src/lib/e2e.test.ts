import { afterEach, describe, expect, it } from "vitest";

import { QUERY_EVENT, REPLY_EVENT, answer, installE2E, windowOf } from "@lib/e2e";

function fakeEvents() {
  const listeners = new Map<string, (e: { data: unknown }) => void>();
  const emitted: [string, unknown][] = [];
  return {
    api: {
      On: (name: string, cb: (e: { data: unknown }) => void) => {
        listeners.set(name, cb);
        return () => listeners.delete(name);
      },
      Emit: (name: string, data?: unknown) => emitted.push([name, data]),
    },
    send: (name: string, data: unknown) => listeners.get(name)?.({ data }),
    listeners,
    emitted,
  };
}

describe("e2e DOM queries", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    window.location.hash = "";
  });

  it("maps every page but the overlay to the settings window", () => {
    expect(windowOf("overlay")).toBe("overlay");
    expect(windowOf("settings")).toBe("settings");
    expect(windowOf("diagnostics")).toBe("settings");
    expect(windowOf("onboarding")).toBe("settings");
  });

  it("reports existence, visibility and text of the first match", () => {
    document.body.innerHTML = `<dd data-testid="price-median">2 div</dd><p hidden id="gone">x</p>`;
    const median = document.querySelector("dd") as Element;
    median.checkVisibility = () => true;
    const q = { id: "1", window: "overlay", selector: '[data-testid="price-median"]' };
    expect(answer(q, document, "overlay")).toEqual({ id: "1", exists: true, visible: true, text: "2 div" });

    const gone = document.getElementById("gone") as Element;
    gone.checkVisibility = () => false;
    expect(answer({ ...q, selector: "#gone" }, document, "overlay")).toMatchObject({ exists: true, visible: false });
    expect(answer({ ...q, selector: "#nope" }, document, "overlay")).toEqual({
      id: "1",
      exists: false,
      visible: false,
      text: "",
    });
  });

  it("falls back to computed style when checkVisibility is missing", () => {
    document.body.innerHTML = `<p id="a" style="display:none">a</p>`;
    Object.defineProperty(document.getElementById("a"), "checkVisibility", { value: undefined });
    expect(answer({ id: "2", window: "settings", selector: "#a" }, document, "settings")?.visible).toBe(false);
  });

  it("ignores queries for other windows, malformed queries and bad selectors", () => {
    document.body.innerHTML = `<p>x</p>`;
    expect(answer({ id: "1", window: "settings", selector: "p" }, document, "overlay")).toBeNull();
    expect(answer({ id: 1, window: "overlay", selector: "p" }, document, "overlay")).toBeNull();
    expect(answer(null, document, "overlay")).toBeNull();
    expect(answer({ id: "3", window: "overlay", selector: "[[" }, document, "overlay")).toMatchObject({
      exists: false,
    });
  });

  it("answers over the event bus only in the app's web view", () => {
    const off = fakeEvents();
    installE2E(off.api, false);
    expect(off.listeners.size).toBe(0);

    const ev = fakeEvents();
    window.location.hash = "#/settings";
    document.body.innerHTML = `<main data-testid="settings-root">Settings</main>`;
    const unsubscribe = installE2E(ev.api, true);
    ev.send(QUERY_EVENT, { id: "7", window: "overlay", selector: "main" });
    expect(ev.emitted).toEqual([]);
    ev.send(QUERY_EVENT, { id: "8", window: "settings", selector: '[data-testid="settings-root"]' });
    expect(ev.emitted).toEqual([[REPLY_EVENT, expect.objectContaining({ id: "8", exists: true, text: "Settings" })]]);
    unsubscribe();
    expect(ev.listeners.size).toBe(0);
  });

  it("stays idle outside the app's web view by default", () => {
    // jsdom is no native web view, so nothing subscribes to the Wails runtime.
    expect(() => installE2E()()).not.toThrow();
  });
});
