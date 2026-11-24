# PoE2 official trade API (`/api/trade2`)

Captured live on **2026-11-05** (UTC 12:00) from `https://www.pathofexile.com` with User-Agent
`divvy-research/0.1 (+https://github.com/BorrowedMirror/divvy)`, no POESESSID. Eight requests total. Cloudflare did not
block us. Trimmed and anonymised copies are in `internal/trade/testdata/fixtures/`.

## Endpoints

| Purpose | Method + path | Rate-limit policy |
| --- | --- | --- |
| Leagues | `GET /api/trade2/data/leagues` | none (Cloudflare-cached, `cache-control: public, max-age=14400`) |
| Stats | `GET /api/trade2/data/stats` | none (CF cached) |
| Static (currency ids) | `GET /api/trade2/data/static` | none (CF cached) |
| Items (bases, uniques) | `GET /api/trade2/data/items` | none (CF cached) |
| Filters | `GET /api/trade2/data/filters` | none (CF cached, not fetched) |
| Item search | `POST /api/trade2/search/poe2/{league}` | `trade-search-request-limit` |
| Fetch listings | `GET /api/trade2/fetch/{id,id,...}?query={search_id}&realm=poe2` (max 10 ids) | `trade-fetch-request-limit` |
| Bulk exchange | `POST /api/trade2/exchange/poe2/{league}` | `trade-exchange-request-limit` |

The search path **includes the realm segment**: `/api/trade2/search/poe2/Standard` returned 200. League names with
spaces are percent-encoded as a path segment (`Forbidden%20Rites`).

Trade site URLs (what the overlay opens in the browser):

- search: `https://www.pathofexile.com/trade2/search/poe2/{league}/{search_id}`
- exchange: `https://www.pathofexile.com/trade2/exchange/poe2/{league}/{search_id}`

## Shapes

### `data/leagues`
```json
{"result":[{"id":"Forbidden Rites","realm":"poe2","text":"Forbidden Rites"},
           {"id":"Standard","realm":"poe2","text":"Standard"}, ...]}
```
Order on the capture date: current challenge league, its HC variant, the previous league (+HC), Standard, Hardcore.

### `data/stats`
`{"result":[{"id":"pseudo","label":"Pseudo","entries":[{"id":"pseudo.pseudo_total_cold_resistance","text":"+#% total to Cold Resistance","type":"pseudo"}]}]}`.
Groups seen: `pseudo explicit implicit fractured crafted enchant rune(label "Augment") desecrated sanctum skill`. Every
entry has exactly `id`, `text`, `type` (no `option` blocks on this date).

### `data/static`
`{"result":[{"id":"Currency","label":"Currency","entries":[{"id":"wisdom","text":"Scroll of Wisdom","image":"/gen/image/..."}]}]}`.
Groups:
`Currency Fragments Verisium Runes Expedition Vaal Delirium Breach Ritual Abyss Essences UncutGems LineageSupportGems Waystones Misc`.
`entries[].id` is the exchange tag (`divine`, `exalted`, `chaos`, ...) and `text` is the in-game name.

### `data/items`
`{"result":[{"id":"accessory","label":"Accessories","entries":[{"type":"Gold Ring"},{"type":"Gold Ring","text":"Andvarius Gold Ring","name":"Andvarius","flags":{"unique":true}}]}]}`.
Some groups also carry `disc` (armour/weapon variants).

### `search/poe2/{league}` request + response
```json
{"query":{"status":{"option":"online"},"name":"Andvarius","type":"Gold Ring","stats":[{"type":"and","filters":[]}]},"sort":{"price":"asc"}}
```
```json
{"id":"H4sIAAAA...","complexity":6,"result":["0033ca90...", "..."],"total":11}
```
`result` holds up to 100 ids; `total` may exceed it.

### `fetch/{ids}?query={id}`
`{"result":[{"id":"...","listing":{...},"item":{...}}]}` - an entry can be `null` when the listing disappeared between
search and fetch.

`listing`: `method` (`psapi`), `indexed` (RFC 3339, `Z`), `stash {name,x,y}`,
`price {type: "~b/o"|"~price", amount, currency}`, `account {name, online, lastCharacterName, language, realm}`,
`whisper`. `account.online` is `null` when offline, `{"league":"Standard"}` when online and
`{"league":"Standard","status":"afk"}` when AFK. The online league can differ from the searched league.

`item`:
`realm verified w h icon league id name typeLine baseType rarity ilvl identified note properties requirements implicitMods explicitMods flavourText frameType frameTypeId extended`.
Stackables also carry `stackSize`. Mods in trade2 are objects
(`{"description","domain","hash","mods":[{"magnitudes":[{"min","max"}]}]}`), unlike PoE1 strings.

### `exchange/poe2/{league}`
Request:
```json
{"query":{"status":{"option":"online"},"have":["exalted"],"want":["divine"],"minimum":1},"sort":{"have":"asc"},"engine":"new"}
```
`have` = what the buyer pays with, `want` = what is being priced (both static ids). Response:
`{"id":"...","complexity":null,"result":{...},"total":N}`. **`result` is an object keyed by listing id**, except when
empty, where it is `[]`.
```json
"<key>":{"id":"...","item":null,"listing":{"indexed":"2026-05-29T12:02:47+00:00",
  "account":{"name":"...","online":null},
  "offers":[{"exchange":{"currency":"exalted","amount":1,"whisper":"{0} Exalted Orb"},
             "item":{"currency":"divine","amount":1,"stock":2,"id":"...","whisper":"{0} Divine Orb"}}]}}
```
Unit price = `exchange.amount / item.amount` in `exchange.currency`; `item.stock` is the seller's stock. No separate
fetch call is needed.

## Rate-limit headers

Every search/fetch/exchange response carries (exposed via `access-control-expose-headers`):

```
x-rate-limit-policy: trade-search-request-limit
x-rate-limit-rules: Ip
x-rate-limit-ip: 5:10:60,15:60:300,30:300:1800,600:21600:3600
x-rate-limit-ip-state: 1:10:0,1:60:0,1:300:0,1:21600:0
```

Rules are `max_hits:period_s:penalty_s`, states are `current_hits:period_s:active_restriction_s`, both comma-separated
and aligned by period. `x-rate-limit-rules` lists the rule sets present (`Ip`, and `Account` when a POESESSID is sent,
per GGG developer docs); each named `X` maps to `x-rate-limit-x` / `x-rate-limit-x-state`.

Observed limits (anonymous, 2026-11-05):

| Policy | Ip rules |
| --- | --- |
| `trade-search-request-limit` | `5:10:60, 15:60:300, 30:300:1800, 600:21600:3600` |
| `trade-fetch-request-limit` | `12:4:10, 16:12:300, 50:300:300, 1000:21600:1800` |
| `trade-exchange-request-limit` | `5:15:60, 10:90:300, 30:300:1800` |

Exceeding a rule returns **HTTP 429** with `Retry-After: <seconds>` and a state whose third field is the remaining
restriction (e.g. `6:10:60`). Not triggered live on purpose; the 429 header fixture (`headers/search.429.txt`) is
synthesised from this format. While restricted, every request extends the penalty, so the client must not send anything
until the restriction expires.

Error bodies are `{"error":{"code":N,"message":"..."}}`.

## Other notes

- Responses set a fresh anonymous `POESESSID` cookie; we never persist it.
- Data endpoints are served from Cloudflare cache with no rate-limit headers.
- GGG asks for a descriptive User-Agent with contact info (`trade.DefaultUserAgent`).
