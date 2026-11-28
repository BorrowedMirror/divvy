import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import i18n from "@i18n/index";
import { setTransport } from "@src/bindings";
import { useOverlayStore } from "@stores/overlay";
import { useSettingsStore } from "@stores/settings";
import { useUpdateStore } from "@stores/update";

// Importing @wailsio/runtime starts a 50 ms window timer (its drag handlers' set-up poll). A test
// file that finishes sooner has its jsdom window torn down first, and the timer then fails with
// "window is not defined" as an unhandled error in whichever file ran fastest. The tests reach the
// backend through setTransport mocks, never the runtime, so it is replaced with an inert stand-in.
vi.mock("@wailsio/runtime", () => ({
  Call: { ByName: () => Promise.reject(new Error("the Wails runtime is not available in tests")) },
  Events: { On: () => () => {} },
}));

// jsdom gaps that Base UI's floating/positioning code touches.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver;
Element.prototype.scrollIntoView ??= function scrollIntoView() {};
window.matchMedia ??= ((query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addListener() {},
  removeListener() {},
  addEventListener() {},
  removeEventListener() {},
  dispatchEvent: () => false,
})) as unknown as typeof window.matchMedia;

beforeEach(async () => {
  window.location.hash = "";
  await i18n.changeLanguage("en");
});

afterEach(() => {
  cleanup();
  setTransport(null);
  useOverlayStore.getState().reset();
  useSettingsStore.setState({ base: null, draft: null, externalChange: false });
  useUpdateStore.setState({ available: null, dismissed: null, installing: false });
});
