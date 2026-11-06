import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import { priceSummary, settings } from "../../test/fixtures";
import { callsOf, renderWithProviders, setupIpc } from "../../test/ipc";
import { Results } from "./Results";

const [first] = priceSummary().listings;
if (!first) throw new Error("fixture has listings");
const base = first;
const summary = priceSummary({
  listings: [
    { ...base, id: "1", seller: "Ann", whisper: "@Ann Hi", price: { amount: 40, currency: "chaos" }, equivalent: { amount: 2, currency: "divine" } },
    { ...base, id: "2", seller: "Ann", whisper: "@Ann Hi again" },
    { ...base, id: "3", seller: "Bob", whisper: null, item_level: 79 },
  ],
});

test("groups by seller, shows equivalents and copies whispers", async () => {
  const user = userEvent.setup();
  const calls = setupIpc({ copy_text: () => null });
  renderWithProviders(<Results summary={summary} now={Date.parse("2026-11-06T00:00:00Z")} />);
  await waitFor(() => expect(screen.getAllByTestId("listing")).toHaveLength(2));
  const [ann, bob] = screen.getAllByTestId("listing") as [HTMLElement, HTMLElement];
  expect(within(ann).getByText("×2")).toBeInTheDocument();
  expect(within(ann).getByTestId("equivalent")).toHaveTextContent("≈ 2");
  expect(within(bob).queryByRole("button", { name: /Copy whisper/ })).not.toBeInTheDocument();
  await user.click(within(ann).getByRole("button", { name: "Copy whisper to Ann" }));
  expect(callsOf(calls, "copy_text")[0]?.args).toEqual({ text: "@Ann Hi" });
  expect(await screen.findByText("Whisper copied")).toBeInTheDocument();
});

test("ungrouped listings and chosen columns", async () => {
  setupIpc({
    get_settings: () => settings({ overlay: { ...settings().overlay, group_by_seller: false, listing_columns: ["item_level"] } }),
  });
  renderWithProviders(<Results summary={summary} />);
  await waitFor(() => expect(screen.getAllByTestId("listing")).toHaveLength(3));
  expect(screen.getByRole("columnheader", { name: "Item level" })).toBeInTheDocument();
  expect(screen.queryByRole("columnheader", { name: "Seller" })).not.toBeInTheDocument();
  const rows = screen.getAllByTestId("listing");
  expect(within(rows[2] as HTMLElement).getByText("79")).toBeInTheDocument();
});
