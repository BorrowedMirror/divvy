import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { clearMocks } from "@tauri-apps/api/mocks";
import { afterEach, beforeEach } from "vitest";
import i18n from "@i18n/index";
import { useOverlayStore } from "@stores/overlay";
import { useSettingsStore } from "@stores/settings";
import { useUpdateStore } from "@stores/update";

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
  clearMocks();
  useOverlayStore.getState().reset();
  useSettingsStore.setState({ base: null, draft: null, externalChange: false });
  useUpdateStore.setState({ available: null, dismissed: null, installing: false });
});
