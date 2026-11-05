import type { Price } from "../bindings";

export function formatAmount(amount: number | null, locale: string): string {
  if (amount === null) return "?";
  return new Intl.NumberFormat(locale, { maximumFractionDigits: Math.abs(amount) < 10 ? 2 : 1 }).format(amount);
}

export function formatPrice(
  price: Price | null | undefined,
  locale: string,
  currencyLabel: (id: string) => string,
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
