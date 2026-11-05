// Minimal trade2 API mock for e2e (mirrors kalandra_testkit::fixtures::TradeMock).
//
//   node e2e/mock-trade/server.ts [--port 18080]
//
// Trade routes (what kalandra-trade calls, base URL via KALANDRA_TRADE_BASE_URL):
//   GET  /api/trade2/data/leagues
//   POST /api/trade2/search/poe2/:league
//   GET  /api/trade2/fetch/:ids?query=:id
// Control routes (used by specs):
//   POST /__mock/config   { leagues?, listings?: [{amount, currency, account?, online?}], total? }
//   POST /__mock/rate-limit { endpoint: "leagues"|"search"|"fetch", times, retryAfter }
//   POST /__mock/reset
//   GET  /__mock/requests  -> [{method, path, body}]
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import type { AddressInfo } from "node:net";

export interface MockListing {
  amount: number;
  currency: string;
  account?: string;
  online?: boolean;
}

export type Endpoint = "leagues" | "search" | "fetch";

export interface MockConfig {
  leagues: string[];
  listings: MockListing[];
  total?: number;
}

interface Injection {
  endpoint: Endpoint;
  remaining: number;
  retryAfter: number;
}

export interface RecordedRequest {
  method: string;
  path: string;
  body: unknown;
}

const here = dirname(fileURLToPath(import.meta.url));
const testkitFixtures = join(here, "..", "..", "crates", "testkit", "fixtures");

/// Default listings: the testkit sample fetch fixture (1, 2, 3 divine -> median 2 divine).
export function defaultConfig(): MockConfig {
  const leagues = JSON.parse(readFileSync(join(testkitFixtures, "leagues.json"), "utf8")) as {
    result: { id: string }[];
  };
  const fetch = JSON.parse(readFileSync(join(testkitFixtures, "fetch.json"), "utf8")) as {
    result: { listing: { price: { amount: number; currency: string }; account: { name: string; online: unknown } } }[];
  };
  return {
    leagues: leagues.result.map((l) => l.id),
    listings: fetch.result.map((r) => ({
      amount: r.listing.price.amount,
      currency: r.listing.price.currency,
      account: r.listing.account.name,
      online: r.listing.account.online !== null,
    })),
  };
}

export const listingId = (i: number): string => (i + 1).toString(16).padStart(64, "0");

const RATE_HEADERS = {
  "x-rate-limit-policy": "trade-search-request-limit",
  "x-rate-limit-rules": "Ip",
  "x-rate-limit-ip": "5:10:60,15:60:300",
};

export class TradeMockServer {
  config: MockConfig = defaultConfig();
  injections: Injection[] = [];
  requests: RecordedRequest[] = [];
  readonly server: Server;

  constructor() {
    this.server = createServer((req, res) => {
      void this.handle(req, res).catch((e: unknown) => {
        send(res, 500, { error: { code: 0, message: String(e) } });
      });
    });
  }

  async listen(port = 0, host = "127.0.0.1"): Promise<string> {
    await new Promise<void>((resolve, reject) => {
      this.server.once("error", reject);
      this.server.listen(port, host, () => resolve());
    });
    return this.url();
  }

  url(): string {
    const a = this.server.address() as AddressInfo;
    return `http://${a.address}:${a.port}`;
  }

  async close(): Promise<void> {
    await new Promise<void>((resolve) => this.server.close(() => resolve()));
  }

  reset(): void {
    this.config = defaultConfig();
    this.injections = [];
    this.requests = [];
  }

  private injected(endpoint: Endpoint): Injection | undefined {
    const inj = this.injections.find((i) => i.endpoint === endpoint && i.remaining > 0);
    if (inj) inj.remaining -= 1;
    return inj;
  }

  private async handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const url = new URL(req.url ?? "/", "http://mock");
    const raw = await readBody(req);
    const body: unknown = raw ? JSON.parse(raw) : null;
    const path = url.pathname;

    if (path.startsWith("/__mock/")) {
      return this.control(req.method ?? "GET", path, body, res);
    }
    this.requests.push({ method: req.method ?? "GET", path: path + url.search, body });

    let endpoint: Endpoint | undefined;
    if (req.method === "GET" && path === "/api/trade2/data/leagues") endpoint = "leagues";
    else if (req.method === "POST" && /^\/api\/trade2\/search\/poe2\/[^/]+$/.test(path)) endpoint = "search";
    else if (req.method === "GET" && /^\/api\/trade2\/fetch\/[^/]+$/.test(path)) endpoint = "fetch";
    if (!endpoint) return send(res, 404, { error: { code: 1, message: "Resource not found" } });

    const inj = this.injected(endpoint);
    if (inj) {
      return send(
        res,
        429,
        { error: { code: 3, message: "Rate limit exceeded" } },
        { ...RATE_HEADERS, "x-rate-limit-ip-state": "6:10:60,6:60:0", "retry-after": String(inj.retryAfter) },
      );
    }
    const ok = (v: unknown) => send(res, 200, v, { ...RATE_HEADERS, "x-rate-limit-ip-state": "1:10:0,1:60:0" });
    const c = this.config;
    switch (endpoint) {
      case "leagues":
        return ok({ result: c.leagues.map((id) => ({ id, realm: "poe2", text: id })) });
      case "search":
        return ok({
          id: "kalandraMock",
          complexity: 4,
          result: c.listings.map((_, i) => listingId(i)),
          total: c.total ?? c.listings.length,
        });
      case "fetch": {
        const ids = decodeURIComponent(path.slice("/api/trade2/fetch/".length)).split(",").filter(Boolean);
        return ok({
          result: ids.map((id) => {
            const i = c.listings.findIndex((_, j) => listingId(j) === id);
            const l = c.listings[i];
            if (!l) return null;
            return {
              id,
              listing: {
                method: "psapi",
                indexed: "2026-11-01T10:00:00Z",
                account: {
                  name: l.account ?? `seller#${String(i + 1).padStart(4, "0")}`,
                  online: l.online === false ? null : { league: c.leagues[0] },
                  lastCharacterName: "MockCharacter",
                },
                price: { type: "~price", amount: l.amount, currency: l.currency },
              },
              item: { name: "", typeLine: "Mock Item", baseType: "Mock Item", frameType: 0 },
            };
          }),
        });
      }
    }
  }

  private control(method: string, path: string, body: unknown, res: ServerResponse): void {
    if (method === "GET" && path === "/__mock/requests") return send(res, 200, this.requests);
    if (method !== "POST") return send(res, 405, { error: "method" });
    switch (path) {
      case "/__mock/reset":
        this.reset();
        return send(res, 200, { ok: true });
      case "/__mock/config":
        this.config = { ...this.config, ...(body as Partial<MockConfig>) };
        return send(res, 200, this.config);
      case "/__mock/rate-limit": {
        const b = body as { endpoint: Endpoint; times?: number; retryAfter?: number };
        this.injections.push({ endpoint: b.endpoint, remaining: b.times ?? 1, retryAfter: b.retryAfter ?? 1 });
        return send(res, 200, { ok: true });
      }
      default:
        return send(res, 404, { error: "unknown control route" });
    }
  }
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let data = "";
    req.setEncoding("utf8");
    req.on("data", (c: string) => (data += c));
    req.on("end", () => resolve(data));
    req.on("error", reject);
  });
}

function send(res: ServerResponse, status: number, body: unknown, headers: Record<string, string> = {}): void {
  res.writeHead(status, { "content-type": "application/json", ...headers });
  res.end(JSON.stringify(body));
}

/// Client for the control routes (used from spec workers, which do not own the server).
export const mockControl = {
  async configure(base: string, cfg: Partial<MockConfig>): Promise<void> {
    await post(base, "/__mock/config", cfg);
  },
  async rateLimit(base: string, endpoint: Endpoint, times: number, retryAfter: number): Promise<void> {
    await post(base, "/__mock/rate-limit", { endpoint, times, retryAfter });
  },
  async reset(base: string): Promise<void> {
    await post(base, "/__mock/reset", {});
  },
  async requests(base: string): Promise<RecordedRequest[]> {
    const r = await fetch(`${base}/__mock/requests`);
    return (await r.json()) as RecordedRequest[];
  },
};

async function post(base: string, path: string, body: unknown): Promise<void> {
  const r = await fetch(base + path, { method: "POST", body: JSON.stringify(body) });
  if (!r.ok) throw new Error(`mock control ${path}: HTTP ${r.status}`);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const i = process.argv.indexOf("--port");
  const port = i > 0 ? Number(process.argv[i + 1]) : Number(process.env.KALANDRA_E2E_MOCK_PORT ?? 18080);
  const m = new TradeMockServer();
  const url = await m.listen(port);
  console.log(`MOCK ${url}`);
}
