# Aggregated price APIs: poe.ninja and poe2scout

Captured live on **2026-11-05** with User-Agent
`rkl-research/0.1 (+https://github.com/nightah/rust-king-ledger)`. Trimmed fixtures live in
`crates/trade/tests/fixtures/{ninja,scout}/`.

## poe.ninja (PoE2)

Docs: <https://poe.ninja/docs/api>. Base `https://poe.ninja`.

| Endpoint | Notes |
| --- | --- |
| `GET /poe2/api/economy/leagues` | `[{"id":"Forbidden Rites","name":"Forbidden Rites"}, ...]`; `id` is the `league` param |
| `GET /poe2/api/economy/exchange/current/overview?league={id}&type={T}` | currency-like items; `T` in `Currency Fragments Abyss UncutGems LineageSupportGems Essences SoulCores Idols Runes Ritual Expedition Delirium Breach Verisium` |
| `GET /poe2/api/economy/stash/current/item/overview?league={id}&type={T}` | uniques; `T` in `UniqueWeapons UniqueArmours UniqueAccessories UniqueFlasks UniqueCharms UniqueJewels UniqueSanctumRelics UniqueTablets PrecursorTablets` |

Policy (from the docs page): no numeric limit published; excessive use gets clients
blocked; responses are HTTP-cached (~5 min, ETags; we saw
`cache-control: public, max-age=1800`), data refreshes roughly hourly; descriptive UA with
contact required; they prefer apps proxy through their own backend. No
rate-limit headers are sent. **We cache each (league, type) response for 10 minutes.**

### Exchange overview shape
```json
{"core":{"items":[{"id":"divine","name":"Divine Orb",...}],
         "rates":{"exalted":724.8,"chaos":9.36},"primary":"divine","secondary":"chaos"},
 "lines":[{"id":"alch","primaryValue":0.005949,"volumePrimaryValue":67.0,
           "maxVolumeCurrency":"divine","maxVolumeRate":168.1,
           "sparkline":{"totalChange":-14.1,"data":[...]}}],
 "items":[{"id":"alch","name":"Orb of Alchemy","image":"...","category":"Currency","detailsId":"orb-of-alchemy"}]}
```
- `primaryValue` is in `core.primary` (divine).
- `core.rates[c]` is how many `c` one `primary` buys (1 divine = 724.8 exalted).
- `lines[].id` is the trade static id; `items[]` maps it to the display name, which
  is what we match against the copied item's base type.

### Stash item overview shape
```json
{"core":{...same as above...},
 "lines":[{"id":360,"itemId":"The Gnashing Sash Wide Belt","detailsId":"...","name":"The Gnashing Sash",
           "baseType":"Wide Belt","category":"Belt","primaryValue":5314,"listingCount":6,
           "corrupted":false,"levelRequired":60,"sparkLine":{...},"implicitModifiers":[...],...}]}
```
`primaryValue` again in divine. Match on `name` + `baseType`.

## poe2scout

Docs: Swagger UI at <https://api.poe2scout.com/swagger/index.html>, spec at
`https://api.poe2scout.com/openapi/v1.json` (`Poe2scout.Api | v1`). Base
`https://api.poe2scout.com`. **JSON keys are PascalCase.** Responses served
behind Cloudflare/Caddy, `content-encoding: zstd` when offered (gzip otherwise), no
rate-limit headers.

| Endpoint | Notes |
| --- | --- |
| `GET /{realm}/Leagues` | `[{"Value":"Forbidden Rites","ShortName":"forbiddenrites","IsCurrent":true,"DivinePrice":966.56,"ChaosDivinePrice":9.27,"BaseCurrencyApiId":"exalted","BaseCurrencyText":"Exalted Orb",...}]` |
| `GET /{realm}/Leagues/{league}/Items` | every priced item in one array (~1300 items, ~500 KB) |
| `GET /{realm}/Leagues/{league}/Currencies/ByCategory?category=&page=&perPage=&referenceCurrency=&search=` | paged, with `PriceLogs` |
| `GET /{realm}/Leagues/{league}/Uniques/ByCategory?category=...` | paged uniques |
| `GET /{realm}/Leagues/{league}/Items/Categories` | `{UniqueCategories, CurrencyCategories}` |
| `GET /{realm}/Leagues/{league}/Currencies/{apiId}`, `/Items/{itemId}`, `.../History`, `/ExchangeSnapshot`, `/SnapshotPairs`, `/ReferenceCurrencies` | details/history (unused) |
| `GET /Realms` | `[{Value, Label, GameApiId, RealmApiId, TradeApiPath, DefaultLeagueValue}]` |

`realm` is `poe2`.

### `/{realm}/Leagues/{league}/Items` entry
```json
{"ItemId":1,"CategoryApiId":"accessory","Text":"Andvarius Gold Ring","Name":"Andvarius","Type":"Gold Ring",
 "ApiId":null,"BaseItemTypeId":null,"CurrentPrice":15,"IconUrl":"..."}
{"ItemId":291,"CategoryApiId":"currency","Text":"Divine Orb","Name":null,"Type":null,
 "ApiId":"divine","BaseItemTypeId":"Metadata/Items/Currency/CurrencyModValues","CurrentPrice":966.56,"IconUrl":"..."}
```
- `CurrentPrice` is in the league's base currency (`BaseCurrencyApiId`, exalted on this date).
- Uniques match on `Name` + `Type`; currency/gems on `Text` (uncut gems are
  `"Uncut Support Gem (Level 2)"`).
- Rates: `DivinePrice` = exalted per divine; `DivinePrice / ChaosDivinePrice` = exalted per chaos.
- Categories seen: `accessory armour weapon flask map jewel sanctum currency fragments runes talismans essences ultimatum breach expedition ritual delirium waystones vaultkeys abyss uncutgems lineagesupportgems incursion idol verisium vaal`.

We fetch `/Leagues` + `/Leagues/{league}/Items` once per league and cache both for 10 minutes.

## Normalisation

Both sources are reduced to `rkl_core::pricing::ExchangeRates` with the doc's
semantics (`rates[c]` = value of 1 `c` in `base`):

- ninja: `base = "divine"`, `rates = {divine: 1, exalted: 1/724.8, chaos: 1/9.36}`
- scout: `base = "exalted"`, `rates = {exalted: 1, divine: 966.56, chaos: 104.2}`

A single price is shown in divine when worth at least 1 divine, otherwise in exalted.
