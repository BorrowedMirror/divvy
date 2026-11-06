import type { Price } from "@src/bindings";
import i18n from "@i18n/index";

/** Locale for `Intl` formatting: the active UI language unless a caller passes one. */
export function activeLocale(): string {
  return i18n.resolvedLanguage ?? i18n.language;
}

export function formatAmount(amount: number | null, locale: string = activeLocale()): string {
  if (amount === null) return "?";
  return new Intl.NumberFormat(locale, { maximumFractionDigits: Math.abs(amount) < 10 ? 2 : 1 }).format(amount);
}

export function formatPrice(
  price: Price | null | undefined,
  locale: string = activeLocale(),
  currencyLabel: (id: string) => string = (id) => id,
): string {
  if (!price) return "—";
  return `${formatAmount(price.amount, locale)} ${currencyLabel(price.currency)}`;
}

export type Age = { unit: "now" | "minutes" | "hours" | "days"; count: number };

/** Coarse age of an RFC 3339 timestamp relative to `now` (ms since epoch). */
export function ageOf(indexed: string | null | undefined, now: number): Age | null {
  if (!indexed) return null;
  const t = Date.parse(indexed);
  if (Number.isNaN(t)) return null;
  const mins = Math.max(0, Math.floor((now - t) / 60_000));
  if (mins < 1) return { unit: "now", count: 0 };
  if (mins < 60) return { unit: "minutes", count: mins };
  const hours = Math.floor(mins / 60);
  if (hours < 24) return { unit: "hours", count: hours };
  return { unit: "days", count: Math.floor(hours / 24) };
}

const RELATIVE_UNIT = { minutes: "minute", hours: "hour", days: "day" } as const;

/** Localised "10m ago" via `Intl.RelativeTimeFormat`; `null` for "now" (callers show a label). */
export function formatAge(age: Age, locale: string = activeLocale()): string | null {
  if (age.unit === "now") return null;
  return new Intl.RelativeTimeFormat(locale, { numeric: "always", style: "narrow" }).format(
    -age.count,
    RELATIVE_UNIT[age.unit],
  );
}

/** Localised date and time of an RFC 3339 timestamp, or `null` when it can't be parsed. */
export function formatDateTime(iso: string | null | undefined, locale: string = activeLocale()): string | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return null;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(t);
}
