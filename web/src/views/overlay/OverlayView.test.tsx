import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, test, vi } from "vitest";
import { useOverlayStore } from "../../stores/overlay";
import { filterSet, ipcError, itemCopied, parsedItem, priceSummary, settings } from "../../test/fixtures";
import { callsOf, emitEvent, fail, flush, renderWithProviders, setupIpc } from "../../test/ipc";
import { cardPosition, OverlayView } from "./OverlayView";
import { Results } from "./Results";

async function mountWithItem(handlers: Parameters<typeof setupIpc>[0] = {}) {
  const calls = setupIpc({ query_price: () => priceSummary(), ...handlers });
  renderWithProviders(<OverlayView />);
  await flush();
  await emitEvent("item-copied", itemCopied());
  return calls;
}

describe("OverlayView", () => {
  test("lookup links open the unique by name and a rare by its base", async () => {
    const user = userEvent.setup();
    const calls = setupIpc({ get_overlay_snapshot: () => itemCopied(), open_lookup: () => null });
    renderWithProviders(<OverlayView />);
    const links = await screen.findByTestId("item-links");
    await user.click(within(links).getByRole("button", { name: "poe2db" }));
    expect(callsOf(calls, "open_lookup")[0]?.args).toEqual({ site: "poe2db", name: parsedItem().base_type });
    await user.click(within(links).getByRole("button", { name: "Craft of Exile" }));
    expect(callsOf(calls, "open_lookup")[1]?.args).toMatchObject({ site: "craft_of_exile" });
  });

  test("only the item links ticked in settings are shown", async () => {
    setupIpc({
      get_overlay_snapshot: () => itemCopied(),
      get_settings: () => settings({ overlay: { ...settings().overlay, links: { poe2db: false, wiki: true, craft_of_exile: false } } }),
    });
    renderWithProviders(<OverlayView />);
    const links = await screen.findByTestId("item-links");
    await waitFor(() => expect(within(links).getAllByRole("button").map((b) => b.textContent)).toEqual(["Wiki"]));
  });

  test("waystones show a verdict and flag mods from the panel", async () => {
    const user = userEvent.setup();
    const [mod] = parsedItem().modifiers;
    if (!mod) throw new Error("fixture has mods");
    const m = (text: string, template: string) => ({ ...mod, text, template });
    const item = parsedItem({
      class: { type: "waystone" },
      category: "waystone",
      modifiers: [
        m("Players have -20% to all maximum Resistances", "Players have -#% to all maximum Resistances"),
        m("+12% increased Pack Size in Map", "#% increased Pack Size in Map"),
        m("Monsters have 41% increased Life", "Monsters have #% increased Life"),
      ],
    });
    const calls = setupIpc({ get_overlay_snapshot: () => itemCopied({ item }), open_settings_tab: () => null });
    renderWithProviders(<OverlayView />);
    const section = await screen.findByTestId("waystone-mods");
    expect(within(section).getByTestId("waystone-verdict")).toHaveAttribute("data-kind", "danger");
    expect(within(section).getByText("Don't run: 1 dangerous mod")).toBeInTheDocument();
    expect(within(section).getAllByRole("listitem").map((r) => r.dataset.flag)).toEqual(["danger", "good", "none"]);
    await user.click(within(section).getByRole("button", { name: "Flag “Monsters have 41% increased Life” as Warn" }));
    await waitFor(() => expect(callsOf(calls, "save_settings")).toHaveLength(1));
    const saved = callsOf(calls, "save_settings")[0]?.args as { settings: { waystone: { flags: Record<string, string> } } };
    expect(saved.settings.waystone.flags["Monsters have #% increased Life"]).toBe("warn");
    await user.click(within(section).getByRole("button", { name: "Edit flags" }));
    expect(callsOf(calls, "open_settings_tab")[0]?.args).toEqual({ tab: "waystones" });
  });

  test("dragging the header reports start, moves and end; Rust moves the card", async () => {
    const calls = setupIpc({ get_overlay_snapshot: () => null, get_overlay_layout: () => ({ cover: true, x: 100, y: 50 }), drag_card: () => null });
    renderWithProviders(<OverlayView />);
    await screen.findByTestId("overlay-backdrop");
    const handle = screen.getByTestId("drag-handle");
    fireEvent.pointerDown(handle, { button: 0, pointerId: 1 });
    fireEvent.pointerMove(handle, { pointerId: 1 });
    fireEvent.pointerUp(handle, { pointerId: 1 });
    expect(callsOf(calls, "drag_card").map((c) => c.args)).toEqual([{ phase: "start" }, { phase: "move" }, { phase: "end" }]);
    await emitEvent("overlay-layout", { cover: true, x: 140, y: 60 });
    await waitFor(() => expect(screen.getByTestId("overlay-card").style.left).toBe("140px"));
  });

  test("pressing a header button does not start a drag", async () => {
    const calls = setupIpc({ get_overlay_snapshot: () => null, drag_card: () => null });
    renderWithProviders(<OverlayView />);
    const close = await screen.findByRole("button", { name: "Close overlay" });
    fireEvent.pointerDown(close, { button: 0, pointerId: 2, screenX: 1, screenY: 1 });
    fireEvent.pointerMove(screen.getByTestId("drag-handle"), { pointerId: 2, screenX: 50, screenY: 50 });
    expect(callsOf(calls, "drag_card")).toHaveLength(0);
  });

  test("unidentified unique: pick a possible unique to search it by name", async () => {
    const user = userEvent.setup();
    const calls = setupIpc({
      get_overlay_snapshot: () =>
        itemCopied({ filters: filterSet({ name: null, rarity: "unique", unique_candidates: ["Andvarius", "Ventor's Gamble"] }) }),
      query_price: () => priceSummary(),
    });
    renderWithProviders(<OverlayView />);
    const group = await screen.findByTestId("unique-candidates");
    expect(within(group).getByRole("button", { name: "Unidentified" })).toHaveAttribute("aria-pressed", "true");
    await user.click(within(group).getByRole("button", { name: "Andvarius" }));
    await waitFor(() => expect(callsOf(calls, "query_price").length).toBeGreaterThanOrEqual(2));
    const last = callsOf(calls, "query_price").at(-1)?.args as { filters: { name: string; misc: { identified: boolean | null } } };
    expect(last.filters.name).toBe("Andvarius");
    expect(last.filters.misc.identified).toBeNull();
    expect(within(group).getByRole("button", { name: "Andvarius" })).toHaveAttribute("aria-pressed", "true");
    await user.click(within(group).getByRole("button", { name: "Unidentified" }));
    await waitFor(() => {
      const again = callsOf(calls, "query_price").at(-1)?.args as { filters: { name: string | null; misc: { identified: boolean | null } } };
      expect([again.filters.name, again.filters.misc.identified]).toEqual([null, false]);
    });
  });

  test("stat rows show the mod tier when known", async () => {
    setupIpc({ get_overlay_snapshot: () => itemCopied() });
    renderWithProviders(<OverlayView />);
    const tiers = await screen.findAllByTestId("mod-tier");
    expect(tiers).toHaveLength(1);
    expect(tiers[0]).toHaveTextContent("T1");
  });

  test("covering the game: dimmed backdrop closes on click, the card does not", async () => {
    const calls = setupIpc({ get_overlay_snapshot: () => null, get_overlay_layout: () => ({ cover: true, x: 300, y: 120 }) });
    renderWithProviders(<OverlayView />);
    const backdrop = await screen.findByTestId("overlay-backdrop");
    const card = screen.getByTestId("overlay-card");
    expect(card.style.left).toBe("300px");
    fireEvent.mouseDown(card);
    expect(callsOf(calls, "hide_overlay")).toHaveLength(0);
    fireEvent.mouseDown(backdrop);
    expect(callsOf(calls, "hide_overlay")).toHaveLength(1);
    await waitFor(() => expect(backdrop.style.background).toContain("0.6"));
    expect(callsOf(calls, "resize_overlay").length).toBeLessThanOrEqual(1);
  });

  test("covering the game: leaving the card does not auto-hide", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const calls = setupIpc({ get_overlay_snapshot: () => null, get_overlay_layout: () => ({ cover: true, x: 0, y: 0 }) });
      renderWithProviders(<OverlayView />);
      await screen.findByTestId("overlay-backdrop");
      const article = screen.getByRole("article");
      fireEvent.mouseEnter(article);
      fireEvent.mouseLeave(article);
      await act(async () => {
        vi.advanceTimersByTime(5000);
      });
      expect(callsOf(calls, "hide_overlay")).toHaveLength(0);
    } finally {
      vi.useRealTimers();
    }
  });

  test("pinned (card-sized window) has no backdrop and follows layout events", async () => {
    setupIpc({ get_overlay_snapshot: () => null });
    renderWithProviders(<OverlayView />);
    await flush();
    expect(screen.queryByTestId("overlay-backdrop")).not.toBeInTheDocument();
    await emitEvent("overlay-layout", { cover: true, x: 10, y: 10 });
    expect(await screen.findByTestId("overlay-backdrop")).toBeInTheDocument();
    await emitEvent("overlay-layout", { cover: false, x: 0, y: 0 });
    await waitFor(() => expect(screen.queryByTestId("overlay-backdrop")).not.toBeInTheDocument());
  });

  test("switching layouts keeps the same card element (its size observers stay attached)", async () => {
    setupIpc({ get_overlay_snapshot: () => null });
    renderWithProviders(<OverlayView />);
    await flush();
    const before = screen.getByTestId("overlay-card");
    await emitEvent("overlay-layout", { cover: true, x: 10, y: 10 });
    await screen.findByTestId("overlay-backdrop");
    expect(screen.getByTestId("overlay-card")).toBe(before);
  });

  test("cardPosition keeps the card inside the window", () => {
    Object.defineProperty(window, "innerWidth", { value: 1920, configurable: true });
    Object.defineProperty(window, "innerHeight", { value: 1080, configurable: true });
    expect(cardPosition({ cover: true, x: 100, y: 200 }, 500)).toEqual({ left: 100, top: 200 });
    expect(cardPosition({ cover: true, x: 1800, y: 900 }, 500)).toEqual({ left: 1492, top: 572 });
    expect(cardPosition({ cover: true, x: -5, y: null }, 500)).toEqual({ left: 0, top: 0 });
  });

  test("shows the empty hint with the configured hotkey", async () => {
    setupIpc();
    renderWithProviders(<OverlayView />);
    expect(await screen.findByText(/press Ctrl\+D/)).toBeInTheDocument();
  });

  test("catches up from the snapshot when item-copied fired before listening", async () => {
    const calls = setupIpc({ query_price: () => priceSummary(), get_overlay_snapshot: () => itemCopied() });
    renderWithProviders(<OverlayView />);
    await flush();
    expect(await screen.findByTestId("price-median")).toHaveTextContent("2.75 div");
    expect(callsOf(calls, "get_overlay_snapshot")).toHaveLength(1);
    expect(callsOf(calls, "query_price")).toHaveLength(1);
  });

  test("snapshot of the item already shown does not re-run the query", async () => {
    useOverlayStore.getState().itemCopied(itemCopied());
    const calls = setupIpc({ query_price: () => priceSummary(), get_overlay_snapshot: () => itemCopied() });
    renderWithProviders(<OverlayView />);
    await flush();
    await waitFor(() => expect(callsOf(calls, "get_overlay_snapshot")).toHaveLength(1));
    await flush();
    expect(callsOf(calls, "query_price")).toHaveLength(0);
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
        get_settings: () => settings({ overlay: { auto_hide_ms: ms, opacity: 1, side: "cursor", group_by_seller: true, listing_columns: ["seller", "listed"], scale: 1, backdrop_dim: 0.6, close_pinned_on_game_focus: false, links: { poe2db: true, wiki: true, craft_of_exile: true } } }),
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
