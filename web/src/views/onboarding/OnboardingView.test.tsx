import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, test, vi } from "vitest";

import { PERMISSION_POLL_MS, permissionPollInterval } from "@hooks/queries";
import type { PermissionInfo_Serialize, PermissionReport_Serialize, Settings_Serialize } from "@src/bindings";
import { callsOf, emitEvent, fail, renderWithProviders, setupIpc } from "@test/ipc";
import { OnboardingView, isBlocked } from "@views/onboarding/OnboardingView";

const URL = "x-apple.systempreferences:com.apple.preference.security?Privacy_";

function perm(kind: string, granted: boolean, required = true): PermissionInfo_Serialize {
  return { kind, granted, required, settings_url: `${URL}${kind}` };
}

function report(overrides: Partial<PermissionReport_Serialize> = {}): PermissionReport_Serialize {
  return { platform: "x11", missing: [], notes: [], gnome: false, ...overrides };
}

const macos = (a: boolean, s: boolean, i: boolean) =>
  report({
    platform: "macos",
    missing: [perm("accessibility", a), perm("screen_recording", s), perm("input_monitoring", i, false)],
  });

function card(kind: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(`li[data-kind="${kind}"]`);
  if (!el) throw new Error(`no card for ${kind}`);
  return el;
}

afterEach(() => {
  vi.useRealTimers();
});

describe("OnboardingView on macOS", () => {
  test("lists permissions, opens System Settings and ticks off grants live", async () => {
    const user = userEvent.setup();
    const calls = setupIpc({ recheck_permissions: () => macos(false, false, false), request_permission: () => null });
    renderWithProviders(<OnboardingView />);

    expect(await screen.findByRole("heading", { name: "Set up RKL" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "Setup for macOS" })).toBeInTheDocument();
    expect(await screen.findByText(/presses the game's copy shortcut/)).toBeInTheDocument();
    expect(within(card("accessibility")).getByText("required")).toBeInTheDocument();
    expect(within(card("input_monitoring")).getByText("optional")).toBeInTheDocument();
    expect(card("accessibility")).toHaveAttribute("data-granted", "false");
    expect(screen.getByText(/Some required permissions are still missing/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue without" })).toBeInTheDocument();

    await user.click(within(card("accessibility")).getByRole("button", { name: "Open System Settings" }));
    expect(callsOf(calls, "request_permission")[0]?.args).toEqual({ kind: "accessibility" });
    expect(within(card("accessibility")).queryByText(/restarts/)).not.toBeInTheDocument();

    await user.click(within(card("screen_recording")).getByRole("button", { name: "Open System Settings" }));
    expect(within(card("screen_recording")).getByText(/after RKL restarts/)).toBeInTheDocument();

    await emitEvent("permissions-changed", { report: macos(true, true, false) });
    await waitFor(() => expect(card("accessibility")).toHaveAttribute("data-granted", "true"));
    expect(within(card("accessibility")).getByText("✓")).toBeInTheDocument();
    expect(within(card("accessibility")).getByText("Granted")).toBeInTheDocument();
    expect(within(card("accessibility")).queryByRole("button")).not.toBeInTheDocument();
    expect(within(card("input_monitoring")).getByText("Not granted")).toBeInTheDocument();
    expect(screen.queryByText(/Some required permissions are still missing/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Done" })).toBeInTheDocument();
  });

  test("polls recheck_permissions every 2 s while visible", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let granted = false;
    const calls = setupIpc({ recheck_permissions: () => macos(granted, true, true) });
    renderWithProviders(<OnboardingView />);
    await waitFor(() => expect(card("accessibility")).toHaveAttribute("data-granted", "false"));
    const before = callsOf(calls, "recheck_permissions").length;
    granted = true;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(PERMISSION_POLL_MS + 50);
    });
    expect(callsOf(calls, "recheck_permissions").length).toBeGreaterThan(before);
    await waitFor(() => expect(card("accessibility")).toHaveAttribute("data-granted", "true"));
  });

  test("request failure is shown", async () => {
    const user = userEvent.setup();
    setupIpc({
      recheck_permissions: () => macos(false, true, true),
      request_permission: () => fail({ kind: "platform", message: "nope", retry_after_ms: null }),
    });
    renderWithProviders(<OnboardingView />);
    await user.click(await screen.findByRole("button", { name: "Open System Settings" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't open System Settings: nope");
  });

  test("unknown permission kinds get a generic label", async () => {
    setupIpc({
      recheck_permissions: () => report({ platform: "macos", missing: [perm("camera", false, false)] }),
    });
    renderWithProviders(<OnboardingView />);
    expect(await screen.findByText("Permission: camera")).toBeInTheDocument();
  });
});

describe("OnboardingView on other platforms", () => {
  test("Windows explains borderless mode and UIPI", async () => {
    setupIpc({ recheck_permissions: () => report({ platform: "windows" }) });
    renderWithProviders(<OnboardingView />);
    expect(await screen.findByText("Use windowed or borderless fullscreen")).toBeInTheDocument();
    expect(screen.getByText(/\(UIPI\)/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open System Settings" })).not.toBeInTheDocument();
  });

  test("Linux X11 needs no setup", async () => {
    setupIpc({ recheck_permissions: () => report({ platform: "x11" }) });
    renderWithProviders(<OnboardingView />);
    expect(await screen.findByText("No extra setup needed")).toBeInTheDocument();
    expect(screen.queryByText("Technical details")).not.toBeInTheDocument();
  });

  test("GNOME on X11 explains the XWayland requirement", async () => {
    setupIpc({ recheck_permissions: () => report({ platform: "x11", gnome: true, notes: ["GNOME: only X11"] }) });
    renderWithProviders(<OnboardingView />);
    expect(await screen.findByText("GNOME: X11/XWayland only")).toBeInTheDocument();
    expect(screen.getByText(/runs through XWayland here/)).toBeInTheDocument();
    expect(screen.getByText("Technical details")).toBeInTheDocument();
    expect(screen.getByText("GNOME: only X11")).toBeInTheDocument();
  });

  test("Wayland-native explains degradation and the --trigger binding", async () => {
    setupIpc({
      recheck_permissions: () => report({ platform: "wayland", notes: ["No GlobalShortcuts portal"] }),
    });
    renderWithProviders(<OnboardingView />);
    expect(await screen.findByText("Wayland-native mode has limits")).toBeInTheDocument();
    expect(screen.getByText(/GlobalShortcuts portal, which asks/)).toBeInTheDocument();
    expect(screen.getByText(/layer-shell instead of following/)).toBeInTheDocument();
    expect(screen.getByText("Bind the price check in your compositor")).toBeInTheDocument();
    expect(screen.getByText("bindsym Ctrl+d exec rkl --trigger")).toBeInTheDocument();
    expect(screen.getByText("Hyprland")).toBeInTheDocument();
    expect(screen.getByText("No GlobalShortcuts portal")).toBeInTheDocument();
    expect(screen.queryByText("GNOME: X11/XWayland only")).not.toBeInTheDocument();
  });

  test("Wayland-native on GNOME warns about layer-shell", async () => {
    setupIpc({ recheck_permissions: () => report({ platform: "wayland", gnome: true }) });
    renderWithProviders(<OnboardingView />);
    expect(await screen.findByText("GNOME: X11/XWayland only")).toBeInTheDocument();
    expect(screen.getByText(/Switch the backend to X11/)).toBeInTheDocument();
  });

  test("load failure", async () => {
    setupIpc({ recheck_permissions: () => fail("x") });
    renderWithProviders(<OnboardingView />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't check permissions.");
  });
});

describe("finishing onboarding", () => {
  test("Done saves onboarding_done and calls onDone", async () => {
    const user = userEvent.setup();
    const onDone = vi.fn();
    const calls = setupIpc({ recheck_permissions: () => report() });
    renderWithProviders(<OnboardingView onDone={onDone} />);
    await screen.findByText("No extra setup needed");
    const done = screen.getByRole("button", { name: "Done" });
    await waitFor(() => expect(done).toBeEnabled());
    await user.click(done);
    await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
    const saved = callsOf(calls, "save_settings")[0]?.args?.settings as Settings_Serialize;
    expect(saved.ui.onboarding_done).toBe(true);
  });

  test("save failure is shown and onDone is not called", async () => {
    const user = userEvent.setup();
    const onDone = vi.fn();
    setupIpc({
      recheck_permissions: () => report(),
      save_settings: () => fail({ kind: "config", message: "disk full", retry_after_ms: null }),
    });
    renderWithProviders(<OnboardingView onDone={onDone} />);
    const done = await screen.findByRole("button", { name: "Done" });
    await waitFor(() => expect(done).toBeEnabled());
    await user.click(done);
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't save: disk full");
    expect(onDone).not.toHaveBeenCalled();
  });
});

describe("helpers", () => {
  test("isBlocked only counts required, missing permissions", () => {
    expect(isBlocked(macos(true, true, false))).toBe(false);
    expect(isBlocked(macos(true, false, true))).toBe(true);
    expect(isBlocked(report())).toBe(false);
  });

  test("polling pauses while the document is hidden", () => {
    const spy = vi.spyOn(document, "visibilityState", "get");
    spy.mockReturnValue("hidden");
    expect(permissionPollInterval()).toBe(false);
    spy.mockReturnValue("visible");
    expect(permissionPollInterval()).toBe(PERMISSION_POLL_MS);
    spy.mockRestore();
  });
});
