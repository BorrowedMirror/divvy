import { useTranslation } from "react-i18next";
import type { ModFlag, ParsedItem_Serialize } from "@src/bindings";
import { commands } from "@src/bindings";
import { cx } from "@components/ui";
import { useCommand } from "@hooks/useCommand";
import { useSettingsQuery } from "@hooks/queries";
import { type Flags, flagFor, toggleFlag, verdictFor } from "@lib/waystone";

const FLAGS: ModFlag[] = ["danger", "warn", "good"];

const ROW: Record<ModFlag, string> = { danger: "bg-bad/10", warn: "bg-warn/10", good: "bg-good/10" };
const TAG: Record<ModFlag, string> = {
  danger: "bg-bad/25 text-bad",
  warn: "bg-warn/25 text-warn",
  good: "bg-good/25 text-good",
};

export function FlagIcon({ flag }: { flag: ModFlag }) {
  const paths: Record<ModFlag, string> = {
    danger: "M7 1.5l5.5 10H1.5z M7 5.5v3 M7 10.2v.1",
    warn: "M7 1.5a5.5 5.5 0 1 0 0 11a5.5 5.5 0 1 0 0-11z M7 4v3.5 M7 9.8v.1",
    good: "M7 1.5l1.7 3.5 3.8.5-2.8 2.6.7 3.8L7 10.1 3.6 11.9l.7-3.8L1.5 5.5l3.8-.5z",
  };
  return (
    <svg viewBox="0 0 14 14" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path d={paths[flag]} strokeLinejoin="round" />
    </svg>
  );
}

/**
 * Waystone mod check in the panel: a verdict banner, then every mod with its flag. The small
 * buttons on each row flag a mod right here; flags apply to every waystone and are listed in
 * Settings → Waystones.
 */
export function WaystoneMods({ item }: { item: ParsedItem_Serialize }) {
  const { t } = useTranslation();
  const command = useCommand();
  const settings = useSettingsQuery().data;
  const mods = item.modifiers.filter((m) => m.kind !== "skill");
  if (!settings || mods.length === 0) return null;
  const flags: Flags = settings.waystone.flags;
  const verdict = verdictFor(
    mods.map((m) => m.template),
    flags,
  );
  const setFlag = (template: string, flag: ModFlag) =>
    void command(commands.saveSettings({ ...settings, waystone: { flags: toggleFlag(flags, template, flag) } }));

  return (
    <section aria-labelledby="waystone-heading" className="flex flex-col gap-2" data-testid="waystone-mods">
      <div
        role="status"
        data-testid="waystone-verdict"
        data-kind={verdict.kind}
        className={cx(
          "flex items-center gap-2.5 rounded-lg border px-3 py-2",
          verdict.kind === "danger" && "border-bad/60 bg-bad/15 text-bad",
          verdict.kind === "warn" && "border-warn/60 bg-warn/15 text-warn",
          verdict.kind === "good" && "border-good/60 bg-good/15 text-good",
          verdict.kind === "clear" && "border-line bg-surface-2 text-muted",
        )}
      >
        {verdict.kind !== "clear" && <FlagIcon flag={verdict.kind} />}
        <div className="flex flex-col">
          <strong className="text-sm">{t(`waystone.verdict.${verdict.kind}`, { count: verdict[verdict.kind === "clear" ? "danger" : verdict.kind] })}</strong>
          <span className="text-xs opacity-80">{t("waystone.summary", { danger: verdict.danger, warn: verdict.warn, good: verdict.good })}</span>
        </div>
      </div>
      <div className="flex items-baseline justify-between">
        <h2 id="waystone-heading" className="text-[11px] font-semibold tracking-wider text-muted uppercase">
          {t("waystone.heading")}
        </h2>
        <button
          type="button"
          className="text-[11px] text-gold underline-offset-2 hover:underline outline-none focus-visible:ring-2 focus-visible:ring-gold"
          onClick={() => void command(commands.openSettingsTab("waystones"))}
        >
          {t("waystone.editFlags")}
        </button>
      </div>
      <ul className="flex flex-col gap-1">
        {mods.map((m, i) => {
          const flag = flagFor(m.template, flags);
          return (
            <li
              // biome-ignore lint/suspicious/noArrayIndexKey: a waystone can roll the same mod text twice.
              key={`${m.text}-${i}`}
              data-flag={flag ?? "none"}
              className={cx("grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 rounded-md px-2 py-1", flag && ROW[flag])}
            >
              <span className="flex items-center gap-2 text-sm">
                {flag && (
                  <span className={cx("rounded px-1.5 py-0.5 text-[10px] font-semibold tracking-wide uppercase", TAG[flag])}>
                    {t(`waystone.flag.${flag}`)}
                  </span>
                )}
                {m.text}
              </span>
              <span className="flex gap-0.5">
                {FLAGS.map((f) => (
                  <button
                    key={f}
                    type="button"
                    aria-label={t("waystone.markAs", { flag: t(`waystone.flag.${f}`), mod: m.text })}
                    aria-pressed={flag === f}
                    title={t(`waystone.flag.${f}`)}
                    onClick={() => setFlag(m.template, f)}
                    className={cx(
                      "grid h-6 w-6 place-items-center rounded border outline-none focus-visible:ring-2 focus-visible:ring-gold",
                      flag === f ? `${TAG[f]} border-transparent` : "border-line text-muted hover:text-fg",
                    )}
                  >
                    <FlagIcon flag={f} />
                  </button>
                ))}
              </span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
