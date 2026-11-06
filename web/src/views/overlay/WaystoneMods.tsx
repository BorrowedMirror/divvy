import { useTranslation } from "react-i18next";
import type { ParsedItem_Serialize } from "../../bindings";
import { cx } from "../../components/ui";
import { classifyMod, type PhraseLists } from "../../lib/waystone";

/** Waystone mod check: every map mod, with the user's avoid / want phrases highlighted. */
export function WaystoneMods({ item, lists }: { item: ParsedItem_Serialize; lists: PhraseLists }) {
  const { t } = useTranslation();
  const mods = item.modifiers.filter((m) => m.kind !== "skill");
  if (mods.length === 0) return null;
  const verdicts = mods.map((m) => classifyMod(m.text, lists));
  const avoid = verdicts.filter((v) => v === "avoid").length;
  const want = verdicts.filter((v) => v === "want").length;
  return (
    <section aria-labelledby="waystone-heading" className="flex flex-col gap-1.5" data-testid="waystone-mods">
      <div className="flex items-center justify-between gap-2">
        <h2 id="waystone-heading" className="text-[11px] font-semibold tracking-wider text-muted uppercase">
          {t("waystone.heading")}
        </h2>
        <span className="flex gap-1.5 text-[11px]">
          {avoid > 0 && <span className="rounded bg-bad/15 px-1.5 text-bad">{t("waystone.avoidCount", { count: avoid })}</span>}
          {want > 0 && <span className="rounded bg-good/15 px-1.5 text-good">{t("waystone.wantCount", { count: want })}</span>}
        </span>
      </div>
      <ul className="flex flex-col gap-1 text-sm">
        {mods.map((m, i) => (
          <li
            key={`${m.text}-${i}`}
            data-verdict={verdicts[i] ?? "none"}
            className={cx(
              "rounded border-l-2 py-0.5 pl-2",
              verdicts[i] === "avoid" && "border-bad bg-bad/10 text-bad",
              verdicts[i] === "want" && "border-good bg-good/10 text-good",
              verdicts[i] === null && "border-line text-fg",
            )}
          >
            {m.text}
          </li>
        ))}
      </ul>
    </section>
  );
}
