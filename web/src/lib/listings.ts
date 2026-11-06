import type { Listing_Serialize } from "@src/bindings";

export type ListingRow = { listing: Listing_Serialize; count: number };

/**
 * One row per seller: their first (cheapest, as listings arrive price-sorted) listing plus how
 * many they have. Listings without a seller are never merged. Order is preserved.
 */
export function groupBySeller(listings: readonly Listing_Serialize[]): ListingRow[] {
  const rows: ListingRow[] = [];
  const bySeller = new Map<string, ListingRow>();
  for (const listing of listings) {
    const seller = listing.seller;
    const existing = seller ? bySeller.get(seller) : undefined;
    if (existing) {
      existing.count += 1;
      continue;
    }
    const row = { listing, count: 1 };
    rows.push(row);
    if (seller) bySeller.set(seller, row);
  }
  return rows;
}

export function ungrouped(listings: readonly Listing_Serialize[]): ListingRow[] {
  return listings.map((listing) => ({ listing, count: 1 }));
}
