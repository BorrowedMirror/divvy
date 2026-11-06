import { useTranslation } from "react-i18next";
import type { ParsedItem_Serialize, Rarity } from "@src/bindings";
import { cx } from "@components/ui";

const RARITY_CLASS: Record<Rarity, string> = {
  normal: "text-rarity-normal",
  magic: "text-rarity-magic",
  rare: "text-rarity-rare",
  unique: "text-rarity-unique",
  currency: "text-rarity-currency",
  gem: "text-rarity-gem",
  other: "text-fg",
};

export function ItemHeader({ item }: { item: ParsedItem_Serialize }) {
  const { t } = useTranslation();
  const title = item.name ?? item.base_type;
  const facts: string[] = [];
  if (item.item_level !== null) facts.push(t("item.ilvl", { value: item.item_level }));
  if (item.quality !== null) facts.push(t("item.quality", { value: item.quality }));
  if (item.gem_level !== null) facts.push(t("item.gemLevel", { value: item.gem_level }));
  if (item.waystone_tier !== null) facts.push(t("item.waystoneTier", { value: item.waystone_tier }));
  if (item.stack_size) facts.push(t("item.stack", { current: item.stack_size.current, max: item.stack_size.max }));

  return (
    <div className="min-w-0" data-testid="item-header">
      <h1 className={cx("truncate text-base font-semibold leading-tight", RARITY_CLASS[item.rarity])} title={title}>
        {title}
      </h1>
      {item.name && <p className="truncate text-xs text-muted">{item.base_type}</p>}
      <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
        <span className="sr-only">{t("item.rarityLabel")}</span>
        <span className={RARITY_CLASS[item.rarity]}>{t(`rarity.${item.rarity}`)}</span>
        {facts.map((f) => (
          <span key={f}>· {f}</span>
        ))}
        {item.corrupted && (
          <span className="rounded border border-bad/60 px-1 text-[10px] font-semibold tracking-wide text-bad uppercase">
            {t("item.corrupted")}
          </span>
        )}
        {item.unidentified && (
          <span className="rounded border border-line px-1 text-[10px] tracking-wide uppercase">
            {t("item.unidentified")}
          </span>
        )}
      </div>
    </div>
  );
}
