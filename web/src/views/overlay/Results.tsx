import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { IpcError_Serialize, PriceSummary_Serialize } from "../../bindings";
import { commands } from "../../bindings";
import { ExternalIcon } from "../../components/icons";
import { PriceTag, useCurrencyIcons } from "../../components/PriceTag";
import { useCommand } from "../../hooks/useCommand";
import { ageOf, formatAge, formatDateTime } from "../../lib/format";

export function Results({ summary, now: nowProp }: { summary: PriceSummary_Serialize; now?: number }) {
  const { t, i18n } = useTranslation();
  const [mountedAt] = useState(() => Date.now());
  const now = nowProp ?? mountedAt;
  const icons = useCurrencyIcons();
  const tradeUrl = summary.trade_url;
  const command = useCommand();

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
                <dd className={k === "median" ? "font-semibold text-gold" : "text-fg"} data-testid={`price-${k}`}>
                  <PriceTag price={p} icons={icons} />
                </dd>
              </div>
            ))}
          </dl>
          <div className="max-h-56 overflow-auto rounded-md border border-line">
            <table className="w-full text-xs">
              <caption className="sr-only">{t("results.listings")}</caption>
              <thead className="sticky top-0 bg-surface-2 text-muted">
                <tr>
                  <th scope="col" className="px-2 py-1 text-left font-medium">
                    {t("results.price")}
                  </th>
                  <th scope="col" className="px-2 py-1 text-left font-medium">
                    {t("results.seller")}
                  </th>
                  <th scope="col" className="px-2 py-1 text-right font-medium">
                    {t("results.age")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {summary.listings.map((l) => {
                  const age = ageOf(l.indexed, now);
                  return (
                    <tr key={l.id} className="border-t border-line/60">
                      <td className="px-2 py-1 tabular-nums">
                        <PriceTag price={l.price} icons={icons} />
                      </td>
                      <td className="max-w-32 truncate px-2 py-1" title={l.seller ?? undefined}>
                        {l.online && (
                          <span className="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-good" aria-hidden="true" />
                        )}
                        {l.seller ?? t("results.unknownSeller")}
                      </td>
                      <td className="px-2 py-1 text-right text-muted" title={formatDateTime(l.indexed, i18n.language) ?? undefined}>
                        {age ? (formatAge(age, i18n.language) ?? t("age.now")) : "—"}
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
