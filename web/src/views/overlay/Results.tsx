import { useState } from "react";

import { Toast } from "@base-ui/react/toast";
import { useTranslation } from "react-i18next";

import { ExternalIcon } from "@components/icons";
import { PriceTag, useCurrencyIcons } from "@components/PriceTag";
import { cx } from "@components/ui";
import { useSettingsQuery } from "@hooks/queries";
import { useCommand } from "@hooks/useCommand";
import type { Age } from "@lib/format";
import { ageOf, formatAge, formatDateTime } from "@lib/format";
import { groupBySeller, ungrouped } from "@lib/listings";
import type { IpcError_Serialize, ListingColumn, Listing_Serialize, PriceSummary_Serialize } from "@src/bindings";
import { commands } from "@src/bindings";

export function Results({ summary, now: nowProp }: { summary: PriceSummary_Serialize; now?: number }) {
  const { t } = useTranslation();
  const [mountedAt] = useState(() => Date.now());
  const now = nowProp ?? mountedAt;
  const icons = useCurrencyIcons();
  const tradeUrl = summary.trade_url;
  const command = useCommand();
  const toast = Toast.useToastManager();
  const settings = useSettingsQuery().data;
  const columns = settings?.overlay.listing_columns ?? DEFAULT_COLUMNS;
  const rows =
    (settings?.overlay.group_by_seller ?? true) ? groupBySeller(summary.listings) : ungrouped(summary.listings);
  const copyWhisper = async (text: string) => {
    const res = await command(commands.copyText(text));
    if (res.status === "ok")
      toast.add({ title: t("results.whisperCopied"), description: t("results.whisperCopiedHelp"), timeout: 3000 });
  };

  return (
    <section aria-labelledby="results-heading" className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h2 id="results-heading" className="text-[11px] font-semibold tracking-wider text-muted uppercase">
          {t("results.heading", { source: t(`source.${summary.source}`) })}
        </h2>
        <span className="flex items-center gap-2">
          <span className="text-xs text-muted" data-testid="total">
            {t("results.total", { count: summary.total })}
          </span>
          {tradeUrl && (
            <button
              type="button"
              onClick={() => void command(commands.openTrade(tradeUrl))}
              className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-gold hover:bg-gold/10 outline-none focus-visible:ring-2 focus-visible:ring-gold"
            >
              {t("results.openTrade")} <ExternalIcon />
            </button>
          )}
        </span>
      </div>
      {summary.listings.length === 0 ? (
        <p className="rounded-md border border-dashed border-line p-3 text-center text-sm text-muted">
          {t("results.empty")}
        </p>
      ) : (
        <>
          <dl className="grid grid-cols-3 gap-1.5 text-center" data-testid="price-summary">
            {(
              [
                ["min", summary.min],
                ["median", summary.median],
                ["max", summary.max],
              ] as const
            ).map(([k, p]) => (
              <div key={k} className="rounded-md border border-line bg-surface-2 px-2 py-1">
                <dt className="text-[10px] tracking-wider text-muted uppercase">{t(`results.${k}`)}</dt>
                <dd
                  className={k === "median" ? "font-semibold text-gold tabular-nums" : "text-fg tabular-nums"}
                  data-testid={`price-${k}`}
                >
                  <PriceTag price={p} icons={icons} size={28} />
                </dd>
              </div>
            ))}
          </dl>
          <div className="overflow-x-auto rounded-md border border-line">
            <table className="w-full text-xs">
              <caption className="sr-only">{t("results.listings")}</caption>
              <thead className="sticky top-0 bg-surface-2 text-muted">
                <tr>
                  <th scope="col" className="px-2 py-1 text-left font-medium">
                    {t("results.price")}
                  </th>
                  {columns.map((c) => (
                    <th
                      key={c}
                      scope="col"
                      className={cx("px-2 py-1 font-medium", c === "listed" ? "text-right" : "text-left")}
                    >
                      {t(`results.column.${c}`)}
                    </th>
                  ))}
                  <th scope="col" className="w-7 px-1 py-1">
                    <span className="sr-only">{t("results.whisper")}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map(({ listing: l, count }) => {
                  const age = ageOf(l.indexed, now);
                  return (
                    <tr key={l.id} className="border-t border-line/60" data-testid="listing">
                      <td className="px-2 py-1 tabular-nums">
                        <span className="flex flex-wrap items-center gap-x-1.5">
                          <PriceTag price={l.price} icons={icons} />
                          {l.equivalent && (
                            <span className="text-[10px] text-muted" data-testid="equivalent">
                              ≈ <PriceTag price={l.equivalent} icons={icons} size={16} />
                            </span>
                          )}
                        </span>
                      </td>
                      {columns.map((c) => (
                        <ListingCell key={c} column={c} listing={l} count={count} age={age} />
                      ))}
                      <td className="px-1 py-1 text-right">
                        {l.whisper && (
                          <button
                            type="button"
                            className="rounded p-0.5 text-muted hover:bg-gold/10 hover:text-gold outline-none focus-visible:ring-2 focus-visible:ring-gold"
                            aria-label={t("results.copyWhisper", { seller: l.seller ?? t("results.unknownSeller") })}
                            title={t("results.copyWhisper", { seller: l.seller ?? t("results.unknownSeller") })}
                            onClick={() => void copyWhisper(l.whisper ?? "")}
                          >
                            <ChatIcon />
                          </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </>
      )}
    </section>
  );
}

const DEFAULT_COLUMNS: ListingColumn[] = ["seller", "listed"];

function ListingCell({
  column,
  listing: l,
  count,
  age,
}: {
  column: ListingColumn;
  listing: Listing_Serialize;
  count: number;
  age: Age | null;
}) {
  const { t, i18n } = useTranslation();
  switch (column) {
    case "seller":
      return (
        <td className="max-w-32 truncate px-2 py-1" title={l.seller ?? undefined}>
          {l.online && <span className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-good" aria-hidden="true" />}
          {l.seller ?? t("results.unknownSeller")}
          {count > 1 && (
            <span
              className="ml-1 rounded bg-surface-3 px-1 text-[10px] text-muted"
              title={t("results.sellerCount", { count })}
            >
              ×{count}
            </span>
          )}
        </td>
      );
    case "item_level":
      return <td className="px-2 py-1 tabular-nums">{l.item_level ?? "—"}</td>;
    case "stock":
      return <td className="px-2 py-1 tabular-nums">{l.stack_size ?? "—"}</td>;
    case "listed":
      return (
        <td className="px-2 py-1 text-right text-muted" title={formatDateTime(l.indexed, i18n.language) ?? undefined}>
          {age ? (formatAge(age, i18n.language) ?? t("age.now")) : "—"}
        </td>
      );
  }
}

function ChatIcon() {
  return (
    <svg
      viewBox="0 0 16 16"
      width="14"
      height="14"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      <path d="M2.5 3.5h11v7h-6l-3 2.5v-2.5h-2z" strokeLinejoin="round" />
    </svg>
  );
}

export function ErrorNotice({ error, secondsLeft }: { error: IpcError_Serialize; secondsLeft: number }) {
  const { t } = useTranslation();
  const rateLimited = error.kind === "rate_limited";
  return (
    <div role="alert" className="rounded-md border border-bad/50 bg-bad/10 p-2.5 text-sm">
      <p className="font-medium text-bad">{t(`errors.${error.kind}`, { defaultValue: t("errors.internal") })}</p>
      <p className="text-xs text-muted">{error.message}</p>
      {rateLimited && secondsLeft > 0 && (
        <p className="mt-1 text-xs text-fg" data-testid="countdown">
          {t("errors.retryIn", { count: secondsLeft })}
        </p>
      )}
    </div>
  );
}
