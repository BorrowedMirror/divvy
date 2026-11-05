import { useTranslation } from "react-i18next";
import type { FilterSet_Serialize, ParsedItem_Serialize } from "../../bindings";
import { CheckboxField, NumberInput, SelectField } from "../../components/ui";
import { type RangeKey, useOverlayStore } from "../../stores/overlay";

function formatValues(values: (number | null)[]): string {
  return values.filter((v): v is number => v !== null).join(" / ");
}

export function StatFilters({ filters }: { filters: FilterSet_Serialize }) {
  const { t } = useTranslation();
  const updateStat = useOverlayStore((s) => s.updateStat);
  if (filters.stats.length === 0) return null;

  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="mb-1 text-[11px] font-semibold tracking-wider text-muted uppercase">
        {t("overlay.stats")}
      </legend>
      {filters.stats.map((s, i) => (
        <div
          key={`${s.stat_id}-${i}`}
          className="grid grid-cols-[minmax(0,1fr)_3.5rem_3.5rem] items-center gap-1.5"
          data-enabled={s.enabled}
        >
          <div className="min-w-0" title={s.text}>
            <CheckboxField
              checked={s.enabled}
              onChange={(enabled) => updateStat(i, { enabled })}
              label={<span className={s.enabled ? "text-fg" : "text-muted"}>{s.text}</span>}
            />
            {s.item_values.length > 0 && (
              <span className="ml-6 block text-[10px] text-muted">
                {t("overlay.itemValue", { value: formatValues(s.item_values) })}
              </span>
            )}
          </div>
          <NumberInput
            label={t("overlay.statMin", { stat: s.text })}
            value={s.min}
            step="any"
            placeholder={t("overlay.min")}
            onChange={(min) => updateStat(i, { min })}
          />
          <NumberInput
            label={t("overlay.statMax", { stat: s.text })}
            value={s.max}
            step="any"
            placeholder={t("overlay.max")}
            onChange={(max) => updateStat(i, { max })}
          />
        </div>
      ))}
    </fieldset>
  );
}

const RANGE_ROWS: { key: RangeKey; label: string; itemValue: (item: ParsedItem_Serialize) => number | null }[] = [
  { key: "item_level", label: "misc.item_level", itemValue: (i) => i.item_level },
  { key: "quality", label: "misc.quality", itemValue: (i) => i.quality },
  { key: "gem_level", label: "misc.gem_level", itemValue: (i) => i.gem_level },
  { key: "waystone_tier", label: "misc.waystone_tier", itemValue: (i) => i.waystone_tier },
  { key: "sockets", label: "misc.sockets", itemValue: (i) => (i.sockets > 0 ? i.sockets : null) },
];

type Tri = "any" | "yes" | "no";
const toTri = (v: boolean | null): Tri => (v === null ? "any" : v ? "yes" : "no");
const fromTri = (v: Tri): boolean | null => (v === "any" ? null : v === "yes");

export function MiscFilters({ filters, item }: { filters: FilterSet_Serialize; item: ParsedItem_Serialize }) {
  const { t } = useTranslation();
  const updateRange = useOverlayStore((s) => s.updateRange);
  const setCorrupted = useOverlayStore((s) => s.setCorrupted);
  const rows = RANGE_ROWS.filter(({ key, itemValue }) => {
    const f = filters.misc[key];
    return f.enabled || f.min !== null || f.max !== null || itemValue(item) !== null;
  });

  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="mb-1 text-[11px] font-semibold tracking-wider text-muted uppercase">
        {t("overlay.misc")}
      </legend>
      {rows.map(({ key, label }) => {
        const f = filters.misc[key];
        return (
          <div key={key} className="grid grid-cols-[minmax(0,1fr)_3.5rem_3.5rem] items-center gap-1.5">
            <CheckboxField checked={f.enabled} onChange={(enabled) => updateRange(key, { enabled })} label={t(label)} />
            <NumberInput
              label={t("overlay.statMin", { stat: t(label) })}
              value={f.min}
              placeholder={t("overlay.min")}
              onChange={(min) => updateRange(key, { min })}
            />
            <NumberInput
              label={t("overlay.statMax", { stat: t(label) })}
              value={f.max}
              placeholder={t("overlay.max")}
              onChange={(max) => updateRange(key, { max })}
            />
          </div>
        );
      })}
      <div className="grid grid-cols-[minmax(0,1fr)_7.5rem] items-center gap-1.5">
        <span className="text-sm">{t("misc.corrupted")}</span>
        <SelectField<Tri>
          label={t("misc.corrupted")}
          hideLabel
          value={toTri(filters.misc.corrupted)}
          onChange={(v) => setCorrupted(fromTri(v))}
          options={[
            { value: "any", label: t("misc.any") },
            { value: "yes", label: t("misc.yes") },
            { value: "no", label: t("misc.no") },
          ]}
        />
      </div>
    </fieldset>
  );
}
