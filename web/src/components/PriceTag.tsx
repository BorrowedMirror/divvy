import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { CurrencyIcon, Price } from "@src/bindings";
import { commands } from "@src/bindings";
import { formatAmount } from "@lib/format";
import { unwrap } from "@lib/ipc";

export const currencyIconsKey = ["currencyIcons"] as const;

/** Exchange tag -> icon, from the trade site's static data (loaded once, then cached). */
export function useCurrencyIcons(): Map<string, CurrencyIcon> {
  const q = useQuery({
    queryKey: currencyIconsKey,
    queryFn: async () => unwrap(await commands.currencyIcons()),
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: Number.POSITIVE_INFINITY,
  });
  const map = new Map<string, CurrencyIcon>();
  for (const c of q.data ?? []) map.set(c.id, c);
  return map;
}

/**
 * A price as "amount + currency icon". The icon is loaded from GGG's CDN (the URL the trade
 * site itself uses; nothing is bundled). Its name is the alt text and tooltip. Without an icon,
 * or if it fails to load, the short currency label is shown instead.
 */
export function PriceTag({
  price,
  icons,
  size = 22,
}: {
  price: Price | null | undefined;
  icons: Map<string, CurrencyIcon>;
  /** Icon edge in px (the CDN images are larger, so they stay sharp). */
  size?: number;
}) {
  const { t, i18n } = useTranslation();
  const [broken, setBroken] = useState(false);
  if (!price) return <>—</>;
  const amount = formatAmount(price.amount, i18n.language);
  const icon = icons.get(price.currency);
  const label = t(`currency.${price.currency}`, { defaultValue: price.currency });
  if (!icon || broken) return <>{`${amount} ${label}`}</>;
  return (
    <span className="inline-flex items-center gap-1 align-middle" title={icon.name}>
      <span>{amount}</span>
      <img
        src={icon.icon}
        alt={icon.name}
        width={size}
        height={size}
        style={{ width: size, height: size }}
        className="inline-block object-contain"
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setBroken(true)}
      />
    </span>
  );
}
