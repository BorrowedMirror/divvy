import { useCallback, useEffect, useRef, useState } from "react";

import { useTranslation } from "react-i18next";

import { CloseIcon, GearIcon, PinIcon } from "@components/icons";
import { Button, IconButton, NumberInput, Segmented, SelectField, Spinner, cx } from "@components/ui";
import { useLeagues, useQueryPrice, useSettingsQuery } from "@hooks/queries";
import { useAutoHide } from "@hooks/useAutoHide";
import { useCommand } from "@hooks/useCommand";
import { useCountdown } from "@hooks/useCountdown";
import { useFitWindowHeight } from "@hooks/useFitWindowHeight";
import { useTauriEvent } from "@hooks/useTauriEvent";
import { formatChord, isUnbound } from "@lib/chord";
import { toIpcError } from "@lib/ipc";
import type { FilterSet_Serialize, ItemCopied_Serialize, OnlineStatus, OverlayLayout } from "@src/bindings";
import { commands, events } from "@src/bindings";
import { useOverlayStore } from "@stores/overlay";
import { MiscFilters, StatFilters } from "@views/overlay/Filters";
import { ItemHeader } from "@views/overlay/ItemHeader";
import { ItemLinks } from "@views/overlay/ItemLinks";
import { ErrorNotice, Results } from "@views/overlay/Results";
import { WaystoneMods } from "@views/overlay/WaystoneMods";

/**
 * Runs `query_price`, discarding responses that a newer query (or a `price-failed` event, via
 * `invalidate`) has superseded.
 */
function usePriceRunner() {
  const mutation = useQueryPrice();
  const seqRef = useRef(0);
  const { mutateAsync } = mutation;
  const invalidate = useCallback(() => {
    seqRef.current += 1;
  }, []);
  const run = useCallback(
    (filters: FilterSet_Serialize) => {
      const id = ++seqRef.current;
      const store = useOverlayStore.getState();
      store.queryStarted();
      mutateAsync(filters).then(
        (summary) => {
          if (id === seqRef.current) useOverlayStore.getState().priceReady(summary);
        },
        (err: unknown) => {
          if (id === seqRef.current) useOverlayStore.getState().priceFailed(toIpcError(err));
        },
      );
    },
    [mutateAsync],
  );
  return { run, invalidate };
}

const isPinned = () => useOverlayStore.getState().pinned;

const sameItem = (a: unknown, b: unknown) => a === b || JSON.stringify(a) === JSON.stringify(b);

export function OverlayView() {
  const { t } = useTranslation();
  const item = useOverlayStore((s) => s.item);
  const filters = useOverlayStore((s) => s.filters);
  const summary = useOverlayStore((s) => s.summary);
  const error = useOverlayStore((s) => s.error);
  const phase = useOverlayStore((s) => s.phase);
  const pinned = useOverlayStore((s) => s.pinned);
  const retryAt = useOverlayStore((s) => s.retryAt);
  const secondsLeft = useCountdown(retryAt);
  const { run, invalidate } = usePriceRunner();
  const settings = useSettingsQuery().data;
  const command = useCommand();
  const hide = useCallback(() => void command(commands.hideOverlay()), [command]);
  const autoHide = useAutoHide(settings?.overlay.auto_hide_ms ?? 0, isPinned, hide);

  const onItemCopied = useCallback(
    (p: ItemCopied_Serialize) => {
      useOverlayStore.getState().itemCopied(p);
      run(p.filters);
    },
    [run],
  );
  // `item-copied` is emitted once; a hotkey pressed before this listener was registered (first
  // second after launch) would be lost, so catch up from the backend's snapshot once listening.
  const catchUp = useCallback(() => {
    commands.getOverlaySnapshot().then(
      (snap) => {
        if (snap && !sameItem(snap.item, useOverlayStore.getState().item)) onItemCopied(snap);
      },
      () => {},
    );
  }, [onItemCopied]);
  useTauriEvent(events.itemCopied, onItemCopied, catchUp);
  useTauriEvent(events.priceReady, (p) => useOverlayStore.getState().priceReady(p.summary));
  useTauriEvent(events.priceFailed, (p) => {
    // Rust shows the overlay on copy/query failure; an in-flight result must not overwrite it.
    invalidate();
    useOverlayStore.getState().priceFailed(p.error);
  });
  useTauriEvent(events.overlayState, (p) => useOverlayStore.getState().setOverlayState(p));

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      if (isPinned()) return;
      hide();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [hide]);

  const togglePin = async () => {
    const next = !pinned;
    const res = await command(commands.setOverlayPinned(next));
    if (res.status === "ok") useOverlayStore.getState().setPinned(next);
  };

  const opacity = settings?.overlay.opacity ?? 1;
  const busy = phase === "loading";
  // The window grows with the number of stats instead of scrolling them.

  // Results beside the filters (two columns) or below them; "auto" goes beside for items with
  // many stats, where the stacked panel would need scrolling.
  const resultsSetting = settings?.overlay.results ?? "below";
  const resultsAt: "below" | "left" | "right" =
    resultsSetting === "auto" ? ((filters?.stats.length ?? 0) > AUTO_SIDE_STATS ? "right" : "below") : resultsSetting;
  const side = resultsAt !== "below" && !!item && !!filters;
  const layout = useOverlayLayout();
  const dragHandlers = useCardDrag();
  // Full content height (not the capped card): Rust places a tall card so it fits on screen.
  const contentRef = useRef<HTMLDivElement | null>(null);
  const fitRef = useFitWindowHeight<HTMLDivElement>(contentRef, () => CARD_CHROME);

  const card = (
    <div
      ref={fitRef}
      data-testid="overlay-card"
      // As a card-sized window (pinned) the card fills the window exactly and the window is
      // shaped to its rounded corners, so compositor shadows (picom) follow the card.
      // No padding in either layout: the saved position is the card's own corner, so pinned and
      // unpinned open at exactly the same spot (the shadow draws outside the card anyway).
      className={layout.cover ? "absolute w-max" : "flex w-max items-start"}
      style={layout.cover ? coverCardStyle(layout, side ? 860 : 420) : undefined}
    >
      <article
        aria-label={t("overlay.title")}
        style={{ opacity }}
        // Covering the game, the panel closes only on a backdrop click, Esc or the hotkey.
        onMouseEnter={layout.cover ? undefined : autoHide.onMouseEnter}
        onMouseLeave={layout.cover ? undefined : autoHide.onMouseLeave}
        className={cx(
          "rounded-xl border border-gold/30 bg-surface p-3 text-fg",
          side ? "w-[860px]" : "w-[420px]",
          // Over the game the card is capped to the screen and scrolls inside itself.
          layout.cover && "max-h-full overflow-y-auto overscroll-contain",
          // A card-sized window gets its rounded shape (and shadow) from the window itself.
          layout.cover && "shadow-2xl",
        )}
      >
        <div ref={contentRef} className="flex flex-col gap-3">
          <header
            className="flex cursor-grab items-start gap-2 select-none active:cursor-grabbing"
            title={t("overlay.dragHint")}
            data-testid="drag-handle"
            {...dragHandlers}
          >
            <div className="min-w-0 flex-1">
              {item ? <ItemHeader item={item} /> : <h1 className="text-base font-semibold text-gold">RKL</h1>}
            </div>
            <IconButton label={t("overlay.openSettings")} onClick={() => void command(commands.openSettings())}>
              <GearIcon />
            </IconButton>
            <IconButton
              label={pinned ? t("overlay.unpin") : t("overlay.pin")}
              pressed={pinned}
              onClick={() => void togglePin()}
            >
              <PinIcon filled={pinned} />
            </IconButton>
            <IconButton label={t("overlay.close")} onClick={hide}>
              <CloseIcon />
            </IconButton>
          </header>
          {item && <ItemLinks item={item} />}

          {!item || !filters ? (
            <div className="flex flex-col gap-3 py-4" aria-live="polite">
              {error && <ErrorNotice error={error} secondsLeft={secondsLeft} />}
              <p className="py-4 text-center text-sm text-muted">
                {t("overlay.empty", {
                  hotkey:
                    settings && !isUnbound(settings.hotkeys.price_check)
                      ? formatChord(settings.hotkeys.price_check)
                      : "Ctrl+D",
                })}
              </p>
            </div>
          ) : (
            <div
              className={side ? "grid grid-cols-2 items-start gap-4" : "flex flex-col gap-3"}
              data-testid="panel-body"
              data-results={resultsAt}
            >
              <div className={cx("flex min-w-0 flex-col gap-3", resultsAt === "left" && "order-2")}>
                <div className="flex flex-col gap-3">
                  {item.category === "waystone" && <WaystoneMods item={item} />}
                  {filters.unique_candidates.length > 0 && <UniquePicker filters={filters} onPick={run} />}
                  <Toolbar filters={filters} />
                  <StatFilters filters={filters} />
                  <MiscFilters filters={filters} item={item} />
                </div>

                <Button
                  variant="primary"
                  onClick={() => run(filters)}
                  disabled={busy || secondsLeft > 0}
                  className="w-full"
                >
                  {secondsLeft > 0 ? t("errors.retryIn", { count: secondsLeft }) : t("overlay.search")}
                </Button>
              </div>

              <div
                className={cx("flex min-w-0 flex-col gap-2", busy && "opacity-70", resultsAt === "left" && "order-1")}
                aria-live="polite"
              >
                {busy && <Spinner label={t("overlay.loading")} />}
                {error && !busy && <ErrorNotice error={error} secondsLeft={secondsLeft} />}
                {summary && !busy && !error && <Results summary={summary} />}
              </div>
            </div>
          )}
        </div>
      </article>
    </div>
  );

  // Over the game the window is a dimmed backdrop and a click on it closes the panel. The card
  // stays at the same place in the tree in both layouts (the wrapper is `display: contents`
  // otherwise), so it is never remounted and its size observers keep watching it.
  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: backdrop click-to-close; Esc is the keyboard path.
    <div
      data-testid={layout.cover ? "overlay-backdrop" : undefined}
      className={layout.cover ? "fixed inset-0" : "contents"}
      style={layout.cover ? { background: `rgb(0 0 0 / ${settings?.overlay.backdrop_dim ?? 0.6})` } : undefined}
      onMouseDown={(e) => {
        if (layout.cover && e.target === e.currentTarget) hide();
      }}
    >
      {card}
    </div>
  );
}

const EDGE = 8;
/** Card padding and border around its content (px). */
const CARD_CHROME = 12 * 2 + 2;
/** How much of a dragged card must stay on screen (px). */
const MIN_VISIBLE = 120;
/** Position plus a height cap, so a card taller than the screen scrolls inside itself. */
export function coverCardStyle(layout: OverlayLayout, width: number) {
  const pos = cardPosition(layout, width);
  return {
    ...pos,
    display: "flex",
    flexDirection: "column" as const,
    maxHeight: Math.max(120, window.innerHeight - pos.top - EDGE),
  };
}

/** `Results: Auto` puts results beside the filters for items with more stats than this. */
export const AUTO_SIDE_STATS = 6;

/** Keep the card inside the covering window horizontally; vertically it may move anywhere that
 * leaves at least `MIN_VISIBLE` px of it on screen (a tall card then gets shorter and scrolls).
 * Rust already places a card that fits so it fits when the panel opens. */
export function cardPosition(layout: OverlayLayout, width = 420): { left: number; top: number } {
  const maxLeft = Math.max(EDGE, window.innerWidth - width - EDGE);
  const maxTop = Math.max(0, window.innerHeight - MIN_VISIBLE);
  return { left: Math.min(Math.max(layout.x ?? 0, 0), maxLeft), top: Math.min(Math.max(layout.y ?? 0, 0), maxTop) };
}

function useOverlayLayout(): OverlayLayout {
  const [layout, setLayout] = useState<OverlayLayout>({ cover: false, x: 0, y: 0 });
  useEffect(() => {
    let live = true;
    void commands.getOverlayLayout().then((l) => {
      if (live) setLayout(l);
    });
    return () => {
      live = false;
    };
  }, []);
  useTauriEvent(events.overlayLayout, setLayout);
  return layout;
}

/**
 * Drag the card by its header. Only the phase is sent: Rust reads the real cursor and moves the
 * window (card-sized) or the card inside the covering window (layout events), and remembers the
 * spot on release for `Position: Last`. Buttons and fields in the header keep working.
 */
export function useCardDrag() {
  const draggingRef = useRef<number | null>(null);
  const send = (phase: "start" | "move" | "end") => void commands.dragCard(phase);
  return {
    onPointerDown: (e: React.PointerEvent<HTMLElement>) => {
      if (e.button !== 0 || (e.target as HTMLElement).closest("button, input, select, a, [role=combobox]")) return;
      draggingRef.current = e.pointerId;
      e.currentTarget.setPointerCapture?.(e.pointerId);
      e.preventDefault();
      send("start");
    },
    onPointerMove: (e: React.PointerEvent<HTMLElement>) => {
      if (draggingRef.current === e.pointerId) send("move");
    },
    onPointerUp: (e: React.PointerEvent<HTMLElement>) => {
      if (draggingRef.current !== e.pointerId) return;
      draggingRef.current = null;
      e.currentTarget.releasePointerCapture?.(e.pointerId);
      send("end");
    },
  };
}

/** Unidentified unique: search the unidentified base, or one of the uniques it can be. */
function UniquePicker({ filters, onPick }: { filters: FilterSet_Serialize; onPick: (f: FilterSet_Serialize) => void }) {
  const { t } = useTranslation();
  const pick = (name: string | null) => {
    useOverlayStore.getState().pickUnique(name);
    const next = useOverlayStore.getState().filters;
    if (next) onPick(next);
  };
  const chip = (label: string, active: boolean, name: string | null) => (
    <button
      key={label}
      type="button"
      aria-pressed={active}
      onClick={() => pick(name)}
      className={cx(
        "rounded-full border px-2 py-0.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-gold",
        active ? "border-gold bg-gold/15 text-gold" : "border-line text-muted hover:text-fg",
      )}
    >
      {label}
    </button>
  );
  return (
    // biome-ignore lint/a11y/useSemanticElements: a labelled row of chips; a fieldset needs a legend and resets.
    <div className="flex flex-col gap-1.5" role="group" aria-label={t("overlay.possibleUniques")}>
      <span className="text-[11px] font-semibold tracking-wider text-muted uppercase">
        {t("overlay.possibleUniques")}
      </span>
      <div className="flex flex-wrap gap-1.5" data-testid="unique-candidates">
        {chip(t("overlay.unidentifiedBase"), filters.name === null, null)}
        {filters.unique_candidates.map((n) => chip(n, filters.name === n, n))}
      </div>
    </div>
  );
}

function Toolbar({ filters }: { filters: FilterSet_Serialize }) {
  const { t } = useTranslation();
  const leagues = useLeagues();
  const setStatus = useOverlayStore((s) => s.setStatus);
  const setLeague = useOverlayStore((s) => s.setLeague);
  const widen = useOverlayStore((s) => s.widen);

  const leagueOptions = (leagues.data ?? []).map((l) => ({ value: l.id, label: l.text ?? l.id }));
  if (!leagueOptions.some((o) => o.value === filters.league)) {
    leagueOptions.unshift({ value: filters.league, label: filters.league });
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-end gap-2">
        <SelectField
          className="min-w-0 flex-1"
          label={t("overlay.league")}
          value={filters.league}
          onChange={setLeague}
          options={leagueOptions}
        />
        <div className="flex w-20 flex-col gap-1">
          <span className="text-xs text-muted" aria-hidden="true">
            {t("overlay.widen")}
          </span>
          <NumberInput
            label={t("overlay.widenAria")}
            value={filters.widen_pct}
            min={0}
            max={100}
            step={5}
            onChange={(v) => widen(v ?? 0)}
          />
        </div>
      </div>
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted">{t("overlay.status")}</span>
        <Segmented<OnlineStatus>
          label={t("overlay.status")}
          value={filters.status}
          onChange={setStatus}
          options={[
            { value: "securable", label: t("status.securable") },
            { value: "online", label: t("status.online") },
            { value: "any", label: t("status.any") },
          ]}
        />
      </div>
    </div>
  );
}
