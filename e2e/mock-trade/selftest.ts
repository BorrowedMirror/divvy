// Self-test for the mock trade server: `node e2e/mock-trade/selftest.ts`. No app needed.
import assert from "node:assert/strict";
import { TradeMockServer, listingId, mockControl } from "./server.ts";

const m = new TradeMockServer();
const base = await m.listen(0);
try {
  const leagues = (await (await fetch(`${base}/api/trade2/data/leagues`)).json()) as { result: { id: string }[] };
  assert.ok(leagues.result.some((l) => l.id === "Standard"));

  const search = async () => {
    const r = await fetch(`${base}/api/trade2/search/poe2/Standard`, { method: "POST", body: "{}" });
    return { status: r.status, headers: r.headers, body: (await r.json()) as { id: string; result: string[]; total: number } };
  };
  const s = await search();
  assert.equal(s.status, 200);
  assert.equal(s.body.result.length, 3);
  assert.equal(s.headers.get("x-rate-limit-rules"), "Ip");

  const f = (await (await fetch(`${base}/api/trade2/fetch/${s.body.result.join(",")}?query=${s.body.id}`)).json()) as {
    result: { listing: { price: { amount: number } } }[];
  };
  assert.deepEqual(
    f.result.map((r) => r.listing.price.amount),
    [1, 2, 3],
  );

  await mockControl.configure(base, { listings: [{ amount: 7, currency: "exalted" }], total: 50 });
  await mockControl.rateLimit(base, "search", 1, 4);
  const limited = await search();
  assert.equal(limited.status, 429);
  assert.equal(limited.headers.get("retry-after"), "4");
  const after = await search();
  assert.equal(after.status, 200);
  assert.equal(after.body.total, 50);
  assert.deepEqual(after.body.result, [listingId(0)]);

  const reqs = await mockControl.requests(base);
  assert.equal(reqs.filter((r) => r.path.startsWith("/api/trade2/search")).length, 3);
  assert.equal((await fetch(`${base}/nope`)).status, 404);

  await mockControl.reset(base);
  assert.equal((await search()).body.result.length, 3);
  console.log("OK: mock-trade selftest passed");
} finally {
  await m.close();
}
