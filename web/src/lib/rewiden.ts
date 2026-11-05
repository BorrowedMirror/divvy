import type { FilterSet_Serialize, StatFilter_Serialize } from "../bindings";

/**
 * Local mirror of `FilterSet::rewiden` (crates/core/src/query.rs).
 *
 * Semantics: for every stat with at least one numeric `item_values` entry, take the mean `v`
 * and set `min = v - |v| * pct / 100` (pct 10 on 100 gives 90). `pct` is clamped to 0..=100.
 * Integer-valued stats are floored so the widened bound stays inclusive; fractional stats are
 * floored to 2 decimals, and the result never exceeds `v`. `max` is cleared (as in Rust);
 * `enabled` is untouched; `widen_pct` is updated.
 */
export function widenValue(values: readonly (number | null)[], pct: number): number | null {
  const nums = values.filter((v): v is number => typeof v === "number" && Number.isFinite(v));
  if (nums.length === 0) return null;
  const mean = nums.reduce((a, b) => a + b, 0) / nums.length;
  const clamped = Math.min(Math.max(pct, 0), 100);
  const raw = mean - (Math.abs(mean) * clamped) / 100;
  const integral = nums.every((n) => Number.isInteger(n)) && Number.isInteger(mean);
  // Epsilon so floating error (e.g. 89.99999999) still floors to the intended value.
  const out = integral ? Math.floor(raw + 1e-9) : Math.floor(raw * 100 + 1e-6) / 100;
  return Math.min(out, mean);
}

export function rewidenStat(stat: StatFilter_Serialize, pct: number): StatFilter_Serialize {
  const min = widenValue(stat.item_values, pct);
  return min === null ? stat : { ...stat, min, max: null };
}

export function rewiden(filters: FilterSet_Serialize, pct: number): FilterSet_Serialize {
  return { ...filters, widen_pct: pct, stats: filters.stats.map((s) => rewidenStat(s, pct)) };
}
