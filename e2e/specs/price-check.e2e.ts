// hotkey -> app sends Ctrl+C to the fake game -> clipboard -> parse -> mock trade2 -> overlay.
import { $, expect } from "@wdio/globals";
import { ITEM_FILE, ITEM_FILE_2, MOCK_URL, triggerMode } from "../lib/env.ts";
import { startFakeGame, type FakeGame } from "../lib/fake-game.ts";
import { hookState, waitForHook, waitForState } from "../lib/hook.ts";
import { switchToWindow, triggerPriceCheck, waitForOverlayReady } from "../lib/app.ts";
import { mockControl } from "../mock-trade/server.ts";

describe(`price check (trigger: ${triggerMode()})`, () => {
  let game: FakeGame | undefined;

  before(async () => {
    await mockControl.reset(MOCK_URL);
    await mockControl.configure(MOCK_URL, {
      listings: [
        { amount: 1, currency: "divine" },
        { amount: 2, currency: "divine" },
        { amount: 3, currency: "divine" },
      ],
      total: 3,
    });
    await waitForHook();
    await waitForOverlayReady();
    game = await startFakeGame({ item: ITEM_FILE });
  });

  after(() => game?.stop());

  it("starts with the overlay hidden", async () => {
    const s = await hookState();
    expect(s.overlay_visible).toBe(false);
  });

  it("copies the item from the game and shows the trade price in the overlay", async () => {
    await triggerPriceCheck(false);
    await game!.waitCopied(1);

    const s = await waitForState((st) => st.overlay_visible && st.last_price !== null, 30000, "overlay with a price");
    expect(s.last_price!.median).toEqual({ amount: 2, currency: "divine" });
    expect(s.last_price!.total).toBe(3);

    const reqs = await mockControl.requests(MOCK_URL);
    expect(reqs.some((r) => r.method === "POST" && r.path.startsWith("/api/trade2/search/poe2/"))).toBe(true);
    expect(reqs.some((r) => r.method === "GET" && r.path.startsWith("/api/trade2/fetch/"))).toBe(true);

    await switchToWindow("overlay");
    const summary = await $('[data-testid="price-summary"]');
    await summary.waitForDisplayed({ timeout: 10000 });
    // The <dl> text has no separators ("MIN1 divMEDIAN2 div..."), so check the median cell.
    await expect($('[data-testid="price-median"]')).toHaveText(expect.stringMatching(/^2(\.0+)?\s*div/));
  });

  it("pinned price check keeps the overlay open", async () => {
    // The app caches trade2 searches by query, so re-checking the same ring would be answered
    // from the cache without reaching the mock. Hover a different ring instead.
    const old = game!;
    const exited = new Promise((r) => (old.proc.exitCode === null ? old.proc.once("exit", r) : r(null)));
    old.stop();
    await exited;
    game = await startFakeGame({ item: ITEM_FILE_2 });
    await mockControl.reset(MOCK_URL);
    await mockControl.configure(MOCK_URL, { listings: [{ amount: 5, currency: "exalted" }], total: 1 });
    await triggerPriceCheck(true);
    await game.waitCopied(1);
    const s = await waitForState(
      (st) => st.overlay_visible && st.last_price?.median?.amount === 5,
      30000,
      "pinned overlay with the new price",
    );
    expect(s.last_price!.median).toEqual({ amount: 5, currency: "exalted" });
    const reqs = await mockControl.requests(MOCK_URL);
    expect(reqs.some((r) => r.method === "POST" && r.path.startsWith("/api/trade2/search/poe2/"))).toBe(true);
    if (s.overlay_pinned !== undefined) expect(s.overlay_pinned).toBe(true);
  });
});
