import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, test, vi } from "vitest";
import { useOverlayStore } from "../../stores/overlay";
import { filterSet, ipcError, itemCopied, parsedItem, priceSummary, settings } from "../../test/fixtures";
import { callsOf, emitEvent, fail, flush, renderWithProviders, setupIpc } from "../../test/ipc";
import { OverlayView } from "./OverlayView";
import { Results } from "./Results";

async function mountWithItem(handlers: Parameters<typeof setupIpc>[0] = {}) {
  const calls = setupIpc({ query_price: () => priceSummary(), ...handlers });
  renderWithProviders(<OverlayView />);
  await flush();
  await emitEvent("item-copied", itemCopied());
  return calls;
}

describe("OverlayView", () => {
  test("shows the empty hint with the configured hotkey", async () => {
    setupIpc();
    renderWithProviders(<OverlayView />);
    expect(await screen.findByText(/press Ctrl\+D/)).toBeInTheDocument();
  });

  test("item-copied renders the item and auto-runs the query", async () => {
    const calls = await mountWithItem();
    const header = screen.getByTestId("item-header");
    expect(within(header).getByRole("heading", { name: "Doom Visor" })).toHaveClass("text-rarity-rare");
    expect(within(header).getByText("Expert Hunter Hood")).toBeInTheDocument();
    expect(within(header).getByText(/ilvl 82/)).toBeInTheDocument();
    expect(within(header).getByText(/20% quality/)).toBeInTheDocument();
    expect(within(header).getByText("Corrupted")).toBeInTheDocument();

    expect(await screen.findByTestId("price-median")).toHaveTextContent("2.75 div");
    expect(screen.getByTestId("total")).toHaveTextContent("57 listings");
    expect(screen.getByText("ExileOne")).toBeInTheDocument();
    expect(screen.getByText("unknown")).toBeInTheDocument();

    const q = callsOf(calls, "query_price");
    expect(q).toHaveLength(1);
    expect(q[0]?.args?.filters).toEqual(filterSet());
  });

  test("editing filters and searching sends the edited FilterSet", async () => {
    const user = userEvent.setup();
    const calls = await mountWithItem();
    await screen.findByTestId("price-median");

    await user.click(screen.getByRole("checkbox", { name: "+32% to Cold Resistance" }));
    const min = screen.getByRole("textbox", { name: "Minimum for +85 to maximum Life" });
    await user.clear(min);
    await user.type(min, "80");
    fireEvent.blur(min);
    await user.click(screen.getByRole("button", { name: "Any" }));
    await user.click(screen.getByRole("button", { name: "Search" }));

    await waitFor(() => expect(callsOf(calls, "query_price")).toHaveLength(2));
    const sent = callsOf(calls, "query_price")[1]?.args?.filters as ReturnType<typeof filterSet>;
    expect(sent.stats[1]?.enabled).toBe(true);
    expect(sent.stats[0]?.min).toBe(80);
    expect(sent.status).toBe("any");
  });

  test("widen control re-applies local rewiden to all stats", async () => {
    await mountWithItem();
    const widen = screen.getByRole("textbox", { name: "Widen stat ranges by percent" });
    fireEvent.change(widen, { target: { value: "20" } });
    fireEvent.blur(widen);
    await waitFor(() => expect(useOverlayStore.getState().filters?.widen_pct).toBe(20));
    expect(useOverlayStore.getState().filters?.stats.map((s) => s.min)).toEqual([68, 25]);
  });

  test("misc filters show item-relevant rows and corrupted select", async () => {
    const user = userEvent.setup();
    await mountWithItem();
    expect(screen.getByRole("checkbox", { name: "Item level" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Quality" })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Sockets" })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Gem level" })).toBeNull();

    await user.click(screen.getByRole("checkbox", { name: "Quality" }));
    expect(useOverlayStore.getState().filters?.misc.quality.enabled).toBe(true);

    await user.click(screen.getByRole("combobox", { name: "Corrupted" }));
    await user.click(await screen.findByRole("option", { name: "No" }));
    expect(useOverlayStore.getState().filters?.misc.corrupted).toBe(false);
  });

  test("league select lists leagues from list_leagues", async () => {
    const user = userEvent.setup();
    await mountWithItem();
    await user.click(screen.getByRole("combobox", { name: "League" }));
    await user.click(await screen.findByRole("option", { name: "Dawn of the Hunt" }));
    expect(useOverlayStore.getState().filters?.league).toBe("Dawn of the Hunt");
  });

  test("open on trade, pin, close", async () => {
    const user = userEvent.setup();
    const calls = await mountWithItem();
    await user.click(await screen.findByRole("button", { name: /Open trade/ }));
    expect(callsOf(calls, "open_trade")[0]?.args).toEqual({ url: priceSummary().trade_url });

    await user.click(screen.getByRole("button", { name: "Pin overlay" }));
    expect(callsOf(calls, "set_overlay_pinned")[0]?.args).toEqual({ pinned: true });
    expect(await screen.findByRole("button", { name: "Unpin overlay" })).toHaveAttribute("aria-pressed", "true");

    await user.click(screen.getByRole("button", { name: "Close overlay" }));
    expect(callsOf(calls, "hide_overlay")).toHaveLength(1);

    await user.click(screen.getByRole("button", { name: "Open settings" }));
    expect(callsOf(calls, "open_settings")).toHaveLength(1);
  });

  test("Esc hides unless pinned", async () => {
    const calls = await mountWithItem();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(callsOf(calls, "hide_overlay")).toHaveLength(1);

    await emitEvent("overlay-state", { visible: true, pinned: true });
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.keyDown(window, { key: "a" });
    expect(callsOf(calls, "hide_overlay")).toHaveLength(1);
  });

  test("pinned set by item-copied is respected", async () => {
    setupIpc({ query_price: () => priceSummary() });
    renderWithProviders(<OverlayView />);
    await flush();
    await emitEvent("item-copied", itemCopied({ pinned: true }));
    expect(screen.getByRole("button", { name: "Unpin overlay" })).toBeInTheDocument();
  });

  test("loading state while the query is in flight", async () => {
    let resolve: (v: unknown) => void = () => {};
    await mountWithItem({ query_price: () => new Promise((r) => (resolve = r)) });
    expect(screen.getByRole("status")).toHaveTextContent("Searching…");
    expect(screen.getByRole("button", { name: "Search" })).toBeDisabled();
    await act(async () => resolve(priceSummary({ listings: [], total: 0, trade_url: null })));
    expect(await screen.findByText(/No listings match/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Open trade/ })).toBeNull();
  });

  test("query errors show the error kind and message", async () => {
    await mountWithItem({ query_price: () => fail(ipcError("trade", "503 from trade2")) });
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("The trade site returned an error");
    expect(alert).toHaveTextContent("503 from trade2");
  });

  test("unknown error kinds fall back to the generic message", async () => {
    await mountWithItem({ query_price: () => fail(ipcError("weird", "??")) });
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong");
  });

  test("rate limit shows a countdown and blocks searching until it ends", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      await mountWithItem({ query_price: () => fail(ipcError("rate_limited", "429", 3000)) });
      expect(await screen.findByTestId("countdown")).toHaveTextContent("Retry in 3 seconds");
      expect(screen.getByRole("button", { name: /Retry in/ })).toBeDisabled();
      await act(async () => {
        vi.advanceTimersByTime(2100);
      });
      expect(screen.getByTestId("countdown")).toHaveTextContent("Retry in 1 second");
      await act(async () => {
        vi.advanceTimersByTime(1500);
      });
      expect(screen.queryByTestId("countdown")).toBeNull();
      expect(screen.getByRole("button", { name: "Search" })).toBeEnabled();
    } finally {
      vi.useRealTimers();
    }
  });

  test("price-ready and price-failed events update the panel", async () => {
    await mountWithItem({ query_price: () => new Promise(() => {}) });
    await emitEvent("price-ready", { summary: priceSummary({ total: 3 }) });
    expect(screen.getByTestId("total")).toHaveTextContent("3 listings");
    await emitEvent("price-failed", { error: { kind: "no_item", message: "nothing copied" } });
    expect(screen.getByRole("alert")).toHaveTextContent("No item on the clipboard");
  });

  test("stale responses from superseded queries are ignored", async () => {
    const resolvers: ((v: unknown) => void)[] = [];
    await mountWithItem({ query_price: () => new Promise((r) => resolvers.push(r)) });
    await emitEvent("item-copied", itemCopied({ item: parsedItem({ name: "Second" }) }));
    await act(async () => resolvers[1]?.(priceSummary({ total: 2 })));
    await act(async () => resolvers[0]?.(priceSummary({ total: 1 })));
    expect(screen.getByTestId("total")).toHaveTextContent("2 listings");
  });

  test("unique items and gems render their own header facts", async () => {
    setupIpc({ query_price: () => new Promise(() => {}) });
    renderWithProviders(<OverlayView />);
    await flush();
    await emitEvent(
      "item-copied",
      itemCopied({
        item: parsedItem({
          rarity: "gem",
          name: null,
          base_type: "Fireball",
          item_level: null,
          quality: null,
          gem_level: 19,
          corrupted: false,
          unidentified: true,
          stack_size: { current: 3, max: 10 },
          waystone_tier: 15,
        }),
        filters: filterSet({ stats: [] }),
      }),
    );
    const header = screen.getByTestId("item-header");
    expect(within(header).getByRole("heading", { name: "Fireball" })).toHaveClass("text-rarity-gem");
    expect(within(header).getByText(/level 19/)).toBeInTheDocument();
    expect(within(header).getByText(/tier 15/)).toBeInTheDocument();
    expect(within(header).getByText(/3\/10/)).toBeInTheDocument();
    expect(within(header).getByText("Unidentified")).toBeInTheDocument();
    expect(screen.queryByText("Stats")).toBeNull();
    expect(screen.getByRole("checkbox", { name: "Gem level" })).toBeInTheDocument();
  });
});

describe("overlay extras", () => {
  test("price summary block exposes the median for e2e", async () => {
    await mountWithItem();
    expect(await screen.findByTestId("price-summary")).toHaveTextContent("2.75");
  });

  test("price-failed without an item renders in the empty state", async () => {
    setupIpc();
    renderWithProviders(<OverlayView />);
    await flush();
    await emitEvent("price-failed", { error: ipcError("no_item", "clipboard empty") });
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("No item on the clipboard");
    expect(alert).toHaveTextContent("clipboard empty");
    expect(screen.getByText(/press Ctrl\+D/)).toBeInTheDocument();
  });

  test("price-failed supersedes an in-flight query result", async () => {
    let resolve: (v: unknown) => void = () => {};
    await mountWithItem({ query_price: () => new Promise((r) => (resolve = r)) });
    await emitEvent("price-failed", { error: ipcError("trade", "upstream down") });
    await act(async () => resolve(priceSummary()));
    expect(screen.getByRole("alert")).toHaveTextContent("upstream down");
    expect(screen.queryByTestId("price-summary")).toBeNull();
  });

  test("failing window commands show a toast", async () => {
    const user = userEvent.setup();
    await mountWithItem({
      hide_overlay: () => fail(ipcError("platform", "cannot hide")),
      open_settings: () => fail(ipcError("platform", "no settings window")),
      open_trade: () => fail(ipcError("platform", "no browser")),
    });
    await user.click(screen.getByRole("button", { name: "Close overlay" }));
    expect(await screen.findByText("cannot hide")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Open settings" }));
    expect(await screen.findByText("no settings window")).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: /Open trade/ }));
    expect(await screen.findByText("no browser")).toBeInTheDocument();
    expect(screen.getAllByText("That action failed").length).toBeGreaterThan(0);
    // Base UI aria-hides the close button until the toast stack is focused/expanded.
    expect(document.querySelectorAll('button[aria-label="Dismiss"]').length).toBeGreaterThan(0);
  });

  describe("auto-hide", () => {
    async function mountAutoHide(ms: number) {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      const calls = setupIpc({
        get_settings: () => settings({ overlay: { auto_hide_ms: ms, opacity: 1, side: "cursor" } }),
        query_price: () => priceSummary(),
      });
      renderWithProviders(<OverlayView />);
      await flush();
      await emitEvent("item-copied", itemCopied());
      await waitFor(() => expect(useOverlayStore.getState().phase).toBe("ready"));
      // Settings query must have resolved so the delay is known.
      await flush();
      return { calls, panel: screen.getByRole("article") };
    }
    const advance = (ms: number) => act(async () => void vi.advanceTimersByTime(ms));

    test("hides after the delay once the pointer leaves", async () => {
      try {
        const { calls, panel } = await mountAutoHide(1000);
        fireEvent.mouseLeave(panel);
        await advance(900);
        expect(callsOf(calls, "hide_overlay")).toHaveLength(0);
        await advance(200);
        expect(callsOf(calls, "hide_overlay")).toHaveLength(1);
      } finally {
        vi.useRealTimers();
      }
    });

    test("re-entering cancels the timer", async () => {
      try {
        const { calls, panel } = await mountAutoHide(1000);
        fireEvent.mouseLeave(panel);
        await advance(500);
        fireEvent.mouseEnter(panel);
        await advance(2000);
        expect(callsOf(calls, "hide_overlay")).toHaveLength(0);
      } finally {
        vi.useRealTimers();
      }
    });

    test("skipped while pinned", async () => {
      try {
        const { calls, panel } = await mountAutoHide(1000);
        fireEvent.mouseLeave(panel);
        await emitEvent("overlay-state", { visible: true, pinned: true });
        await advance(1500);
        fireEvent.mouseLeave(panel);
        await advance(1500);
        expect(callsOf(calls, "hide_overlay")).toHaveLength(0);
      } finally {
        vi.useRealTimers();
      }
    });

    test("disabled when the delay is 0", async () => {
      try {
        const { calls, panel } = await mountAutoHide(0);
        fireEvent.mouseLeave(panel);
        await advance(5000);
        expect(callsOf(calls, "hide_overlay")).toHaveLength(0);
      } finally {
        vi.useRealTimers();
      }
    });
  });
});

describe("Results", () => {
  test("listing ages are relative to now", () => {
    setupIpc();
    renderWithProviders(<Results summary={priceSummary()} now={Date.parse("2026-11-05T12:00:00Z")} />);
    expect(screen.getByText("10m ago")).toBeInTheDocument();
    expect(screen.getByText("1d ago")).toBeInTheDocument();
  });
});
