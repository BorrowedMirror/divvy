import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { App } from "./App";
import i18n from "./i18n";
import { routeFromHash } from "./route";
import { settings } from "./test/fixtures";
import { emitEvent, flush, setupIpc } from "./test/ipc";

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
  setupIpc({ get_settings: () => settings({ ui: { language: "de", theme: "light", start_minimised: false } }) });
  render(<App />);
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("light"));
  await waitFor(() => expect(i18n.language).toBe("de"));
  expect(await screen.findByRole("heading", { name: "Einstellungen" })).toBeInTheDocument();
  await flush();
  await emitEvent("settings-changed", { settings: settings({ ui: { language: "en", theme: "dark", start_minimised: false } }) });
  await waitFor(() => expect(document.documentElement.dataset.theme).toBe("dark"));
  expect(i18n.language).toBe("en");
});
