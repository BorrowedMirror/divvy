import { useCallback, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import type { FilterSet_Serialize, OnlineStatus } from "../../bindings";
import { commands, events } from "../../bindings";
import { CloseIcon, GearIcon, PinIcon } from "../../components/icons";
import { Button, IconButton, NumberInput, Segmented, SelectField, Spinner, cx } from "../../components/ui";
import { useLeagues, useQueryPrice, useSettingsQuery } from "../../hooks/queries";
import { useAutoHide } from "../../hooks/useAutoHide";
import { useCommand } from "../../hooks/useCommand";
import { useCountdown } from "../../hooks/useCountdown";
import { useTauriEvent } from "../../hooks/useTauriEvent";
import { formatChord } from "../../lib/chord";
import { toIpcError } from "../../lib/ipc";
import { useOverlayStore } from "../../stores/overlay";
import { MiscFilters, StatFilters } from "./Filters";
import { ItemHeader } from "./ItemHeader";
import { ErrorNotice, Results } from "./Results";

/**
 * Runs `query_price`, discarding responses that a newer query (or a `price-failed` event, via
 * `invalidate`) has superseded.
 */
function usePriceRunner() {
  const mutation = useQueryPrice();
  const seq = useRef(0);
  const { mutateAsync } = mutation;
  const invalidate = useCallback(() => {
    seq.current += 1;
  }, []);
  const run = useCallback(
    (filters: FilterSet_Serialize) => {
      const id = ++seq.current;
      const store = useOverlayStore.getState();
      store.queryStarted();
      mutateAsync(filters).then(
        (summary) => {
          if (id === seq.current) useOverlayStore.getState().priceReady(summary);
        },
        (err: unknown) => {
          if (id === seq.current) useOverlayStore.getState().priceFailed(toIpcError(err));
        },
      );
    },
    [mutateAsync],
  );
  return { run, invalidate };
}

const isPinned = () => useOverlayStore.getState().pinned;

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

  useTauriEvent(events.itemCopied, (p) => {
    useOverlayStore.getState().itemCopied(p);
    run(p.filters);
  });
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

  return (
    <div className="flex h-screen w-full items-start justify-center p-1.5">
      <article
        aria-label={t("overlay.title")}
        style={{ opacity }}
        onMouseEnter={autoHide.onMouseEnter}
        onMouseLeave={autoHide.onMouseLeave}
        className="flex max-h-full w-full max-w-[420px] flex-col gap-3 overflow-hidden rounded-xl border border-gold/30 bg-surface p-3 text-fg shadow-2xl"
      >
        <header className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            {item ? <ItemHeader item={item} /> : <h1 className="text-base font-semibold text-gold">kalandra</h1>}
          </div>
          <IconButton label={t("overlay.openSettings")} onClick={() => void command(commands.openSettings())}>
            <GearIcon />
          </IconButton>
          <IconButton label={pinned ? t("overlay.unpin") : t("overlay.pin")} pressed={pinned} onClick={() => void togglePin()}>
            <PinIcon filled={pinned} />
          </IconButton>
          <IconButton label={t("overlay.close")} onClick={hide}>
            <CloseIcon />
          </IconButton>
        </header>

        {!item || !filters ? (
          <div className="flex flex-col gap-3 py-4" aria-live="polite">
            {error && <ErrorNotice error={error} secondsLeft={secondsLeft} />}
            <p className="py-4 text-center text-sm text-muted">
              {t("overlay.empty", {
                hotkey: settings ? formatChord(settings.hotkeys.price_check) : "Ctrl+D",
              })}
            </p>
          </div>
        ) : (
          <>
            <div className="flex max-h-[38vh] min-h-24 shrink-0 flex-col gap-3 overflow-y-auto pr-1">
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

            <div className={cx("flex min-h-0 flex-1 flex-col gap-2", busy && "opacity-70")} aria-live="polite">
              {busy && <Spinner label={t("overlay.loading")} />}
              {error && !busy && <ErrorNotice error={error} secondsLeft={secondsLeft} />}
              {summary && !busy && !error && <Results summary={summary} />}
            </div>
          </>
        )}
      </article>
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
