import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import type { ProtonMouseStatus } from "../bindings";
import { callsOf, fail, renderWithProviders, setupIpc } from "../test/ipc";
import { ProtonMouseCard } from "./ProtonMouseCard";

const base: ProtonMouseStatus = {
  supported: true,
  prefix: "/home/u/.local/share/Steam/steamapps/compatdata/2694490/pfx",
  grab_pointer: null,
  grab_fullscreen: null,
  capture_disabled: false,
  can_restore: false,
  game_running: false,
};

test("renders nothing where Proton is not in use", async () => {
  setupIpc({ get_proton_mouse_status: () => ({ ...base, supported: false }) });
  const { container } = renderWithProviders(<ProtonMouseCard />);
  await waitFor(() => expect(container).toBeEmptyDOMElement());
});

test("turns capture off, then restores it", async () => {
  const user = userEvent.setup();
  let current = base;
  const calls = setupIpc({
    get_proton_mouse_status: () => current,
    set_proton_mouse_capture: (a) => {
      current = a?.disabled
        ? { ...base, grab_pointer: "N", grab_fullscreen: "N", capture_disabled: true, can_restore: true }
        : base;
      return current;
    },
  });
  renderWithProviders(<ProtonMouseCard />);
  expect(await screen.findByTestId("proton-status")).toHaveTextContent("Mouse capture: on");
  await user.click(screen.getByRole("button", { name: "Turn off mouse capture" }));
  expect(callsOf(calls, "set_proton_mouse_capture")[0]?.args).toEqual({ disabled: true });
  expect(await screen.findByText("Mouse capture: off")).toBeInTheDocument();
  expect(screen.getByText(/Start Path of Exile 2 again/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Turn off mouse capture" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Restore previous setting" }));
  expect(callsOf(calls, "set_proton_mouse_capture")[1]?.args).toEqual({ disabled: false });
  expect(await screen.findByText("Mouse capture: on (Wine default)")).toBeInTheDocument();
});

test("asks to quit the game first and disables the buttons while it runs", async () => {
  setupIpc({ get_proton_mouse_status: () => ({ ...base, game_running: true, can_restore: true }) });
  renderWithProviders(<ProtonMouseCard />);
  expect(await screen.findByText(/Quit Path of Exile 2 first/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Turn off mouse capture" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Restore previous setting" })).toBeDisabled();
});

test("a refused change keeps the current state", async () => {
  const user = userEvent.setup();
  setupIpc({
    get_proton_mouse_status: () => base,
    set_proton_mouse_capture: () => fail({ kind: "busy", message: "quit first", retry_after_ms: null }),
  });
  renderWithProviders(<ProtonMouseCard />);
  await user.click(await screen.findByRole("button", { name: "Turn off mouse capture" }));
  expect(screen.getByTestId("proton-status")).toHaveTextContent("Mouse capture: on");
  expect(screen.queryByText(/Start Path of Exile 2 again/)).not.toBeInTheDocument();
});
