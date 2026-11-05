import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, test } from "vitest";
import { App } from "../../App";
import { useUpdateStore } from "../../stores/update";
import { ipcError } from "../../test/fixtures";
import { callsOf, emitEvent, fail, flush, renderWithProviders, setupIpc } from "../../test/ipc";
import { UpdateBanner } from "./UpdateBanner";

const update = { version: "0.2.0", notes: "Faster price checks.", date: "2026-11-01T00:00:00Z" };

async function mount(handlers: Parameters<typeof setupIpc>[0] = {}) {
  const calls = setupIpc(handlers);
  renderWithProviders(<UpdateBanner />);
  await flush();
  return calls;
}

describe("UpdateBanner", () => {
  test("hidden until an update is announced", async () => {
    await mount();
    expect(screen.queryByRole("region", { name: "App update" })).not.toBeInTheDocument();
    await emitEvent("update-available", update);
    expect(screen.getByRole("status")).toHaveTextContent("kalandra 0.2.0 is available.");
    expect(screen.getByText("What's new")).toBeInTheDocument();
    expect(screen.getByText("Faster price checks.")).toBeInTheDocument();
  });

  test("notes are optional", async () => {
    await mount();
    await emitEvent("update-available", { ...update, notes: null });
    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(screen.queryByText("What's new")).not.toBeInTheDocument();
  });

  test("install calls install_update and locks the buttons", async () => {
    const user = userEvent.setup();
    const calls = await mount({ install_update: () => new Promise(() => {}) });
    await emitEvent("update-available", update);
    await user.click(screen.getByRole("button", { name: "Install and restart" }));
    expect(callsOf(calls, "install_update")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Installing…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Later" })).toBeDisabled();
  });

  test("a failed install toasts the error and re-enables install", async () => {
    const user = userEvent.setup();
    await mount({ install_update: () => fail(ipcError("update", "signature mismatch")) });
    await emitEvent("update-available", update);
    await user.click(screen.getByRole("button", { name: "Install and restart" }));
    expect(await screen.findByText("signature mismatch")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Install and restart" })).toBeEnabled());
  });

  test("later hides this version but a newer one shows again", async () => {
    const user = userEvent.setup();
    await mount();
    await emitEvent("update-available", update);
    await user.click(screen.getByRole("button", { name: "Later" }));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    await emitEvent("update-available", update);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    await emitEvent("update-available", { ...update, version: "0.3.0" });
    expect(screen.getByRole("status")).toHaveTextContent("kalandra 0.3.0 is available.");
  });

  test("shown on the settings route only, not the overlay", async () => {
    setupIpc();
    useUpdateStore.setState({ available: update });
    window.location.hash = "#/overlay";
    const { unmount } = render(<App />);
    await flush();
    expect(screen.queryByRole("button", { name: "Install and restart" })).not.toBeInTheDocument();
    unmount();
    window.location.hash = "#/settings";
    render(<App />);
    expect(await screen.findByRole("button", { name: "Install and restart" })).toBeInTheDocument();
  });
});
