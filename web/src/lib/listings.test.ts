import { expect, test } from "vitest";

import { groupBySeller, ungrouped } from "@lib/listings";
import { priceSummary } from "@test/fixtures";

test("groupBySeller keeps each seller's first listing with a count and never merges unknown sellers", () => {
  const [base] = priceSummary().listings;
  if (!base) throw new Error("fixture has listings");
  const l = (id: string, seller: string | null) => ({ ...base, id, seller });
  const rows = groupBySeller([l("1", "A"), l("2", "B"), l("3", "A"), l("4", null), l("5", null), l("6", "A")]);
  expect(rows.map((r) => [r.listing.id, r.count])).toEqual([
    ["1", 3],
    ["2", 1],
    ["4", 1],
    ["5", 1],
  ]);
  expect(ungrouped([l("1", "A"), l("2", "A")]).map((r) => r.count)).toEqual([1, 1]);
});
