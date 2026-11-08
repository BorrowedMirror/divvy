import type { CSSProperties } from "react";
import { useState } from "react";

import { useTranslation } from "react-i18next";

import { BrandMark } from "@components/BrandMark";
import { useBackendEvent } from "@hooks/useBackendEvent";
import { events } from "@src/bindings";

/** How long the notice shows before the first `ready-notice` event says otherwise. */
const DEFAULT_DURATION_MS = 4000;

/**
 * The ready notice: a small card shown over the game window once RKL has found it. The
 * window ignores the mouse and never takes focus; each `ready-notice` replays the fade in and out.
 */
export function NoticeView() {
  const { t } = useTranslation();
  const [run, setRun] = useState({ id: 0, ms: DEFAULT_DURATION_MS });
  useBackendEvent(events.readyNotice, (p) => setRun((r) => ({ id: r.id + 1, ms: p.duration_ms })));
  return (
    <output
      key={run.id}
      className="k-notice"
      data-testid="ready-notice"
      style={{ "--k-notice-ms": `${run.ms}ms` } as CSSProperties}
    >
      <BrandMark size={36} />
      <span className="flex min-w-0 flex-col gap-1">
        <span className="truncate font-semibold leading-tight text-gold-bright">Rust King Ledger</span>
        <span className="truncate text-xs leading-tight text-muted">{t("notice.ready")}</span>
      </span>
    </output>
  );
}
