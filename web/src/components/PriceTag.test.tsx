import { fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, test } from "vitest";
import { renderWithProviders, setupIpc } from "../test/ipc";
import { PriceTag, useCurrencyIcons } from "./PriceTag";

const divine = { id: "divine", name: "Divine Orb", icon: "https://web.poecdn.com/gen/image/x/Divine.png" };

function Harness({ currency }: { currency: string }) {
  const icons = useCurrencyIcons();
  return (
    <span data-testid="tag">
      <PriceTag price={{ amount: 2, currency }} icons={icons} />
    </span>
  );
}

test("shows the amount with the currency icon, named for screen readers", async () => {
  setupIpc({ currency_icons: () => [divine] });
  renderWithProviders(<Harness currency="divine" />);
  const img = await screen.findByRole("img", { name: "Divine Orb" });
  expect(img).toHaveAttribute("src", divine.icon);
  expect(img).toHaveAttribute("width", "22");
  expect(screen.getByTestId("tag")).toHaveTextContent("2");
  expect(screen.getByTitle("Divine Orb")).toBeInTheDocument();
});

test("falls back to the text label without an icon or when it fails to load", async () => {
  setupIpc({ currency_icons: () => [divine] });
  const { unmount } = renderWithProviders(<Harness currency="exalted" />);
  await waitFor(() => expect(screen.getByTestId("tag")).toHaveTextContent("2 ex"));
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
  unmount();
  renderWithProviders(<Harness currency="divine" />);
  fireEvent.error(await screen.findByRole("img", { name: "Divine Orb" }));
  await waitFor(() => expect(screen.queryByRole("img")).not.toBeInTheDocument());
  expect(screen.getByTestId("tag")).toHaveTextContent(/^2 \S+/);
});

test("no price renders a dash", () => {
  setupIpc();
  renderWithProviders(<PriceTag price={null} icons={new Map()} />);
  expect(screen.getByText("—")).toBeInTheDocument();
});
