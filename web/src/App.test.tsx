import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, test } from "vitest";
import { App } from "./App";
import i18n from "./i18n";
import { routeFromHash } from "./route";
import { settings } from "./test/fixtures";
import { callsOf, emitEvent, flush, setupIpc } from "./test/ipc";

test("routeFromHash", () => {
  expect(routeFromHash("#/overlay")).toBe("overlay");
  expect(routeFromHash("#/diagnostics")).toBe("diagnostics");
  expect(routeFromHash("#/overlay?x=1")).toBe("overlay");
  expect(routeFromHash("")).toBe("settings");
});

test("renders settings by default and navigates to diagnostics and back", async () => {
  const user = userEvent.setup();
  setupIpc();
  render(<App />);
  expect(screen.getByRole("main")).toHaveAttribute("data-route", "settings");
  await user.click(await screen.findByRole("button", { name: "Diagnostics" }));
  expect(screen.getByRole("main")).toHaveAttribute("data-route", "diagnostics");
  expect(await screen.findByRole("heading", { name: "Diagnostics" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Settings/ }));
  expect(screen.getByRole("main")).toHaveAttribute("data-route", "settings");
});

test("overlay route marks the document transparent", async () => {
  window.location.hash = "#/overlay";
  setupIpc();
  render(<App />);
  expect(screen.getByRole("main")).toHaveAttribute("data-route", "overlay");
  await waitFor(() => expect(document.documentElement.dataset.route).toBe("overlay"));
});

test("hashchange switches routes", async () => {
  setupIpc();
  render(<App />);
  await act(async () => {
    window.location.hash = "#/diagnostics";
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
  expect(screen.getByRole("main")).toHaveAttribute("data-route", "diagnostics");
});

test("applies theme and language from settings, and follows settings-changed", async () => {
  setupIpc({ get_settings: () => settings({ ui: { language: "de", theme: "light", start_minimised: false, check_updates: true, onboarding_done: false } }) });
  render(<App />);
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("light"));
  await waitFor(() => expect(i18n.language).toBe("de"));
  expect(await screen.findByRole("heading", { name: "Einstellungen" })).toBeInTheDocument();
  await flush();
  await emitEvent("settings-changed", { settings: settings({ ui: { language: "en", theme: "dark", start_minimised: false, check_updates: true, onboarding_done: false } }) });
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("dark"));
  expect(i18n.language).toBe("en");
});

test("onboarding route: diagnostics links to it and Done returns to settings", async () => {
  const user = userEvent.setup();
  window.location.hash = "#/diagnostics";
  setupIpc({ recheck_permissions: () => ({ platform: "x11", missing: [], notes: [], gnome: false }) });
  render(<App />);
  expect(routeFromHash("#/onboarding")).toBe("onboarding");
  const link = await screen.findByRole("link", { name: "Open the setup guide" });
  expect(link).toHaveAttribute("href", "#/onboarding");
  await act(async () => {
    window.location.hash = "#/onboarding";
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
  expect(screen.getByRole("main")).toHaveAttribute("data-route", "onboarding");
  const done = await screen.findByRole("button", { name: "Done" });
  await waitFor(() => expect(done).toBeEnabled());
  await user.click(done);
  await waitFor(() => expect(screen.getByRole("main")).toHaveAttribute("data-route", "settings"));
});

describe("settings as a game overlay", () => {
  afterEach(() => {
    delete document.documentElement.dataset.overlay;
  });

  test("Esc and a backdrop click close the settings overlay", async () => {
    window.location.hash = "#/settings";
    document.documentElement.dataset.overlay = "1";
    const calls = setupIpc({ close_settings: () => null });
    render(<App />);
    const shell = await screen.findByTestId("settings-shell");
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(callsOf(calls, "close_settings")).toHaveLength(1);
    // Clicks inside the card do not close it.
    await userEvent.setup().click(screen.getByRole("dialog"));
    expect(callsOf(calls, "close_settings")).toHaveLength(1);
    await act(async () => {
      shell.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    });
    expect(callsOf(calls, "close_settings")).toHaveLength(2);
    await userEvent.setup().click(screen.getByRole("button", { name: "Close overlay" }));
    expect(callsOf(calls, "close_settings")).toHaveLength(3);
  });

  test("a normal settings window ignores Esc and backdrop clicks", async () => {
    window.location.hash = "#/settings";
    const calls = setupIpc({ close_settings: () => null });
    render(<App />);
    const shell = await screen.findByTestId("settings-shell");
    await act(async () => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
      shell.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    });
    expect(callsOf(calls, "close_settings")).toHaveLength(0);
  });

  test("an already handled Escape (popup, hotkey recorder) is ignored", async () => {
    window.location.hash = "#/settings";
    document.documentElement.dataset.overlay = "1";
    const calls = setupIpc({ close_settings: () => null });
    render(<App />);
    await screen.findByTestId("settings-shell");
    await act(async () => {
      const e = new KeyboardEvent("keydown", { key: "Escape", cancelable: true });
      e.preventDefault();
      window.dispatchEvent(e);
    });
    expect(callsOf(calls, "close_settings")).toHaveLength(0);
  });
});
