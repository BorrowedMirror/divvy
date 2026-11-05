import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, test, vi } from "vitest";
import i18n from "../../i18n";
import { diagnostics } from "../../test/fixtures";
import { fail, renderWithProviders, setupIpc } from "../../test/ipc";
import { DiagnosticsView, buildReport, guidanceFor } from "./DiagnosticsView";

describe("DiagnosticsView", () => {
  test("renders facts, capability checklist, guidance and errors", async () => {
    setupIpc();
    renderWithProviders(<DiagnosticsView />);
    expect(await screen.findByRole("heading", { name: "Diagnostics" })).toBeInTheDocument();
    expect(screen.getByText("0.1.0-rc.1")).toBeInTheDocument();
    expect(screen.getByText("0.3.1")).toBeInTheDocument();

    const tracking = screen.getByText("Game window tracking").closest("li");
    expect(tracking).toHaveAttribute("data-ok", "true");
    expect(within(tracking as HTMLElement).getByText("supported")).toBeInTheDocument();
    const inject = screen.getByText("Keystroke injection (copy item)").closest("li");
    expect(inject).toHaveAttribute("data-ok", "false");
    expect(within(inject as HTMLElement).getByText("✗")).toBeInTheDocument();

    expect(screen.getByText("Overlay uses NSPanel")).toBeInTheDocument();
    expect(screen.getByText(/Privacy & Security → Accessibility/)).toBeInTheDocument();
    expect(screen.getByText(/Privacy & Security → Screen Recording/)).toBeInTheDocument();
    expect(screen.getByText("trade: 503 Service Unavailable")).toBeInTheDocument();
  });

  test("copy report writes to the clipboard and toasts", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    setupIpc();
    renderWithProviders(<DiagnosticsView />);
    await user.click(await screen.findByRole("button", { name: "Copy report" }));
    expect(writeText).toHaveBeenCalledOnce();
    expect(writeText.mock.calls[0]?.[0]).toContain("kalandra 0.1.0-rc.1");
    expect(await screen.findByText("Report copied to clipboard")).toBeInTheDocument();
  });

  test("copy failure is reported", async () => {
    const user = userEvent.setup();
    setupIpc();
    renderWithProviders(<DiagnosticsView />);
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(new Error("denied"));
    await user.click(await screen.findByRole("button", { name: "Copy report" }));
    expect(await screen.findByText("Couldn't access the clipboard")).toBeInTheDocument();
  });

  test("load failure", async () => {
    setupIpc({ get_diagnostics: () => fail("x") });
    renderWithProviders(<DiagnosticsView />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load diagnostics.");
  });

  test("no errors and no guidance on a healthy Windows install", async () => {
    const d = diagnostics({
      os: "windows",
      backend: "windows",
      recent_errors: [],
      data_version: null,
      capabilities: { ...diagnostics().capabilities, missing_permissions: [], notes: [] },
    });
    setupIpc({ get_diagnostics: () => d });
    renderWithProviders(<DiagnosticsView />);
    expect(await screen.findByText("No errors recorded.")).toBeInTheDocument();
    expect(screen.queryByText("Missing permissions")).toBeNull();
  });
});

describe("guidance + report", () => {
  test("wayland, gnome/x11 and unknown permissions", () => {
    const base = diagnostics().capabilities;
    expect(guidanceFor(diagnostics({ os: "linux", backend: "wayland", capabilities: { ...base, missing_permissions: [] } }))).toEqual([
      "guidance.wayland",
    ]);
    expect(guidanceFor(diagnostics({ os: "linux", backend: "x11", capabilities: { ...base, missing_permissions: ["input"] } }))).toEqual([
      "permission:input",
      "guidance.gnome",
    ]);
  });

  test("report lists capabilities and placeholders", () => {
    const report = buildReport(diagnostics({ recent_errors: [], league: null }), i18n.t);
    expect(report).toContain("[x] window_tracking");
    expect(report).toContain("[ ] passive_hook");
    expect(report).toContain("League: -");
    expect(report).toContain("accessibility, screen_recording");
    expect(report).toMatch(/Recent errors:\n {2}-/);
  });

  test("generic permission guidance renders", async () => {
    setupIpc({
      get_diagnostics: () =>
        diagnostics({ capabilities: { ...diagnostics().capabilities, missing_permissions: ["input_monitoring"] } }),
    });
    renderWithProviders(<DiagnosticsView />);
    expect(await screen.findByText("Missing permission: input_monitoring")).toBeInTheDocument();
  });
});
