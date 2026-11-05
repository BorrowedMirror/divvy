# kalandra contracts

This file is the source of truth for every public interface fixed in Phase 1.
Workstreams **implement** these; they do not change signatures. If a contract is
genuinely wrong, make the smallest additive change (new method with a default impl,
new optional field with `#[serde(default)]`), document it under "Contract changes"
at the bottom of this file in your branch, and say so in your final report.

## Data flow

```
hotkey (platform::HotkeyHost) ──► desktop: copy flow (platform::copy::copy_item_text)
   ──► core::parser::parse_item ──► core::matcher::StatMatcher (data::GameData: Catalog)
   ──► core::query::build_filters ──► events::ItemCopied ──► web overlay
web edits FilterSet ──► ipc::query_price ──► trade::PriceSource::price ──► events::PriceReady
```

## Crates

### `kalandra-core` (crates/core) — pure, no I/O
| Item | Contract |
| --- | --- |
| `item::{ParsedItem, ItemClass, ItemCategory, Rarity, Modifier, ModKind, AdvancedModInfo, Affix, ValueRange, Property, Requirements, ItemStats, StackSize}` | domain types; serde + specta |
| `ItemClass::from_header(&str) -> ItemClass` | maps `Item Class:` value; unknown -> `Other(raw)`; weapons -> `Weapon(raw)` |
| `ItemClass::category(&self, Rarity) -> ItemCategory` | routing category (unique rarity -> `Unique` unless currency/gem) |
| `parser::parse_item(&str) -> Result<ParsedItem, ParseError>` | Ctrl+C and Ctrl+Alt+C formats, CRLF or LF |
| `parser::looks_like_item(&str) -> bool` | cheap pre-check used by the clipboard poller |
| `parser::SEPARATOR` | `--------` |
| `catalog::{StatDef, StatAlias, BaseDef, UniqueDef, StaticDef}` | data model |
| `catalog::Catalog` trait | `stats_by_template`, `base`, `base_in_magic_name`, `unique`, `static_item` |
| `matcher::templatize(&str) -> String` | numbers -> `#` |
| `matcher::StatMatcher::{new, match_item}` | fills `Modifier::stat_id`, returns `Vec<MatchOutcome>` |
| `query::{QueryOptions, FilterSet, StatFilter, RangeFilter, MiscFilters, OnlineStatus, QueryRoute, TradeQuery}` | editable filters (IPC) and trade2 body |
| `query::build_filters(&ParsedItem, &QueryOptions) -> FilterSet` | defaults incl. widening |
| `FilterSet::rewiden(&mut self, pct)`, `FilterSet::to_trade_query(&self) -> TradeQuery` | |
| `pricing::{Price, Listing, PriceSourceKind, PriceSummary, ExchangeRates}` | |
| `pricing::summarise(kind, total, listings, Option<&ExchangeRates>) -> PriceSummary` | min/median/max, outlier trim |
| `error::{ParseError, CoreError}` | |

### `kalandra-data` (crates/data)
| Item | Contract |
| --- | --- |
| `SCHEMA_VERSION`, `Manifest`, `AssetEntry`, `Bundle`, `DataError` | asset format: `assets/manifest.json` + `assets/{stats,bases,uniques,statics}.json.zst` |
| `decode_asset<T>(&AssetEntry, &[u8]) -> Result<T, DataError>` | zstd decode + sha256 verify + JSON |
| `bundled() -> Result<GameData, DataError>` | assets via `include_bytes!` |
| `load_dir(&Path) -> Result<GameData, DataError>` | |
| `GameData::{from_bundle, bundle, manifest}` + `impl Catalog` | indexed lookups (HashMap by template/name) |

### `kalandra-trade` (crates/trade)
| Item | Contract |
| --- | --- |
| `TradeConfig { base_url, user_agent, poesessid, fetch_count, cache_ttl }` (+ `Default`) | |
| `TradeClient::new(TradeConfig, Box<dyn Clock>) -> Result<Self, TradeError>` | |
| `TradeClient::{leagues, data, search, fetch, trade_url}` | `/api/trade2/data/leagues`, `/api/trade2/data/{which}`, `/api/trade2/search/poe2/{league}`, `/api/trade2/fetch/{ids}?query=`; verify exact paths against the live API, record in docs/research/trade2.md |
| `PriceSource` trait: `kind`, `supports`, `async price(&PriceRequest)` | implemented by `TradeClient`, `ninja::PoeNinjaSource`, `scout::Poe2ScoutSource` |
| `ratelimit::{Clock, SystemClock, Rule, RuleState, parse_rules, parse_states, RateLimiter::{new, reserve, observe}}` | header-driven; Retry-After on 429 |
| `types::{League, ResultList, SearchResponse, FetchEntry, FetchListing, FetchPrice, FetchAccount}` | wire types |
| `error::{TradeError, TradeErrorInfo}` | |
| `default_user_agent()` | descriptive UA with contact; settings may append `trade.contact` |

### `kalandra-platform` (crates/platform)
| Item | Contract |
| --- | --- |
| traits `WindowTracker`, `HotkeyHost`, `InputListener`, `InputSimulator`, `Clipboard`, `OverlayWindow` | `mockall::automock` under `cfg(test)` |
| `Platform { kind, capabilities, window_tracker, hotkeys, input_listener, input_simulator, clipboard }` | bundle returned by a backend |
| `types::{Rect, WindowInfo, WindowEvent, KeyModifier, Chord (parse/Display), HotkeyAction, HotkeyEvent, InputEvent, Capabilities, GAME_WINDOW_TITLE}` | |
| `detect::{BackendKind, EnvProbe::from_env, detect(&EnvProbe, Option<BackendKind>) -> BackendKind}` | override > `KALANDRA_BACKEND` > OS; Linux: X11 if `DISPLAY` set, Wayland-native only without X or on request; GNOME => X11 |
| `copy::{CopyOptions, copy_item_text(&dyn InputSimulator, &dyn Clipboard, CopyOptions, &dyn Fn(Duration)) -> Result<String, PlatformError>}` | save clipboard, clear, send chord, poll until `looks_like_item`-style text or timeout, restore |
| `backend::create(BackendKind) -> Result<Platform, PlatformError>` | dispatches to `backend::{windows,macos,x11,wayland}::create()` (feature `native`) |
| each backend module also exposes `pub fn overlay(raw_window_handle::RawWindowHandle, raw_window_handle::RawDisplayHandle) -> Result<Box<dyn OverlayWindow>, PlatformError>` where applicable | |
| `PlatformError` | |

The single global passive hook (`rdev`) is shared by all non-Wayland backends: put it in
`backend/hook.rs` (owned by F; D and E reuse it). Hook callbacks only `try_send` into a channel.

### `kalandra-config` (crates/config)
| Item | Contract |
| --- | --- |
| `Settings` + sections `LeagueSettings, HotkeySettings, TradeSettings, OverlaySettings, OverlaySide, UiSettings, Theme, PlatformSettings` | TOML, `#[serde(default)]` everywhere |
| `SETTINGS_VERSION`, `migrate(toml::Table)`, `validate(&Settings)`, `load(&Paths)`, `save(&Paths, &Settings)` | atomic save; unknown newer version -> `TooNew` |
| `Paths { config_dir, cache_dir, log_dir, settings_file }`, `Paths::resolve()`, `Paths::under(root)` | `directories::ProjectDirs::from("au", "nerv", "kalandra")`; `KALANDRA_CONFIG_DIR` override |
| `ConfigError` | |

### `kalandra-testkit` (crates/testkit)
| Item | Contract |
| --- | --- |
| `corpus::{CorpusEntry, corpus_dir, load, get}` | format documented in `src/corpus.rs`; `corpus/index.toml` + `corpus/<class>/<slug>.txt` |
| `fakes::*` | `FakeClipboard`, `FakeInputSimulator` (scripted: on chord, write given text to a `FakeClipboard`), `FakeWindowTracker`, `FakeHotkeyHost` (with `fire(action)`), `FakeInputListener`, `FakeOverlay`, `fake_platform() -> Platform` |
| `fixtures::*` | `mount_trade_fixtures(&MockServer)`, `fixture(name) -> serde_json::Value` reading `crates/trade/tests/fixtures/*.json` |

### `apps/desktop` (Tauri 2)
IPC commands (Rust `src/ipc.rs`, exported to `web/src/bindings.ts` via tauri-specta; the web
**never** calls `invoke()` directly — eslint enforces it):

| Command | Signature |
| --- | --- |
| `get_settings` | `() -> Settings` |
| `save_settings` | `(settings: Settings) -> Result<Settings, IpcError>` |
| `get_diagnostics` | `() -> Diagnostics` |
| `list_leagues` | `() -> Result<Vec<League>, IpcError>` |
| `price_check` | `(request: PriceCheckRequest { text: Option<String>, pinned }) -> Result<PriceCheckResponse { item, filters }, IpcError>` |
| `query_price` | `(filters: FilterSet) -> Result<PriceSummary, IpcError>` |
| `open_trade` | `(url: String) -> Result<(), IpcError>` |
| `hide_overlay` | `() -> Result<(), IpcError>` |
| `set_overlay_pinned` | `(pinned: bool) -> Result<(), IpcError>` |
| `open_settings` | `() -> Result<(), IpcError>` |
| `rebind_hotkey` | `(action: HotkeyAction, chord: Chord) -> Result<(), IpcError>` |

Events (`src/events.rs`): `item-copied` `ItemCopied { item, filters, pinned }`,
`price-ready` `PriceReady { summary }`, `price-failed` `PriceFailed { error: IpcError }`,
`settings-changed` `SettingsChanged { settings }`, `overlay-state` `OverlayState { visible, pinned }`.

`IpcError { kind, message, retry_after_ms }`; kinds: `parse`, `trade`, `rate_limited`, `config`,
`platform`, `no_item`, `internal`.

Windows (labels): `overlay` (`index.html#/overlay`, transparent, undecorated, always on top,
hidden by default) and `settings` (`index.html#/settings`; diagnostics at `#/diagnostics`).

Per-OS native overlay styling: `apps/desktop/src-tauri/src/overlay/{windows,macos,linux}.rs`,
each `pub fn prepare(&tauri::WebviewWindow) -> Result<(), String>`.

Bindings: `cargo xtask gen` (or `KALANDRA_UPDATE_BINDINGS=1 cargo test -p kalandra-desktop --test bindings`)
regenerates `web/src/bindings.ts`; the `bindings` test fails when stale.

Debug-only test hook (feature `test-hook`): env `KALANDRA_TEST_HOOK=<port>` exposes a localhost
TCP line protocol: `trigger <pinned:0|1>`, `state` -> JSON `{overlay_visible, last_price}`,
`quit`. Used by the macOS harness and as a fallback trigger for Wayland (`kalandra --trigger`
over the same socket + SIGUSR2).

### `web/`
React 19 + TS strict + Vite + Tailwind v4 + Base UI (`@base-ui/react`) + Zustand + TanStack Query
+ react-i18next. Route from hash: `overlay | settings | diagnostics` (`src/route.ts`).
Tests: Vitest + RTL + `mockIPC` from `@tauri-apps/api/mocks`, coverage >= 80% lines.

### `tools/fake-game`
winit window titled `Path of Exile 2`. CLI: `--item <path>` (normal text), `--advanced-item <path>`
(optional). On Ctrl+C puts `--item` text on the clipboard; on Ctrl+Alt+C puts `--advanced-item`
(or `--item`). Prints `READY` on stdout once the window is mapped, `COPIED <n>` per copy.

### `xtask`
`cargo xtask gen` (commitlint + commit docs from `commit-config.toml`, bindings),
`data refresh [--offline]`, `coverage` (per-crate gates), `release`, `lint`.

## Gates
- Line coverage (cargo llvm-cov): core, trade, data, config >= 90%; platform >= 70%
  (`#[cfg_attr(coverage_nightly, coverage(off))]` only with a justification comment).
- Web >= 80% lines. Patch coverage >= 80%.
- `cargo clippy --workspace --all-targets -- -D warnings`, `cargo fmt --check`, eslint, tsc.

## Ownership (Phase 2)
| WS | Owns |
| --- | --- |
| A core | `crates/core/**`, `crates/testkit/corpus/**` |
| B trade | `crates/trade/**`, `docs/research/trade2.md`, `docs/research/ninja-scout.md` |
| C data | `crates/data/**`, `xtask/src/data/**` (+ the `Data` arm in `xtask/src/main.rs`), `docs/research/data-sources.md` |
| D windows | `crates/platform/src/backend/windows*`, `apps/desktop/src-tauri/src/overlay/windows.rs`, `crates/platform/tests/windows_*.rs` |
| E macos | `crates/platform/src/backend/macos*`, `apps/desktop/src-tauri/src/overlay/macos.rs`, `crates/platform/tests/macos_*.rs`, `docs/research/macos.md` |
| F linux + platform common | `crates/platform/src/{lib,types,detect,copy,error}.rs`, `crates/platform/src/backend/{mod,hook,x11*,wayland*}`, `crates/platform/Cargo.toml`, `apps/desktop/src-tauri/src/overlay/linux.rs`, `crates/platform/tests/linux_*.rs` |
| G web | `web/**` (except `web/src/bindings.ts`, generated) |
| H testkit/e2e | `crates/testkit/{src,Cargo.toml}`, `tools/fake-game/**`, `e2e/**` |
| I repo/CI | `.github/**`, `xtask/src/{main.rs,coverage.rs,release.rs,lint.rs}`, `deny.toml`, `about.toml`, `about.hbs`, `cliff.toml`, `docs/**` (except research files above), `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `renovate.json`, `lefthook.yml`, `commit-config.toml`, `codecov.yml` |
| J config | `crates/config/**` |
| Lead | `Cargo.toml`, `apps/desktop/**` (except overlay OS files), `CONTRACTS.md`, `README.md`, merges |

Workstreams needing a new workspace dependency add it to the root `Cargo.toml`
`[workspace.dependencies]` **only** (append-only, one line per dep) and note it in their report.

## Contract changes
(append here)
- config (J): additive `Paths::resolve_with(env: impl Fn(&str) -> Option<String>)` (injectable
  env lookup; `Paths::resolve()` delegates to it) and `pub const CONFIG_DIR_ENV: &str = "KALANDRA_CONFIG_DIR"`.
- **kalandra-data (C, additive):** `DataError::Io(String, std::io::Error)` variant (missing /
  unreadable asset files in `load_dir`); `GameData::manifest()` accessor alongside the `pub
  manifest` field; `bundled_static() -> &'static GameData` (process-wide cache, `bundled()`
  now returns a cheap `Arc`-backed clone of it); `MANIFEST_FILE`, `sha256_hex`,
  `encode_asset` (inverse of `decode_asset`), `parse_manifest` (JSON + schema check),
  `write_dir(&Path, &Bundle, &AssetSources, generated_at, game_version) -> Result<Manifest, _>`
  and `AssetSources`, shared by `xtask data refresh` and tests.
- **kalandra-data (C, semantics):** `Catalog::stats_by_template` searches `StatDef::kind` =
  trade2 stats group id: `Implicit->implicit`, `Explicit|Sanctified->explicit`,
  `Enchant->enchant`, `Rune->rune`, `Crafted->crafted`, `Fractured->fractured`,
  `Desecrated->desecrated`, `Skill->skill`. Explicit trade templates are unsigned
  (`# to maximum Life`); aliases `+# ...` (negate=false) and `-# ...` (negate=true) are
  generated, plus `#% increased`<->`#% reduced` / `#% more`<->`#% less` (negate=true). The
  matcher should therefore extract unsigned numbers and apply `negate`.
  `base_in_magic_name` matches whole-word runs only, longest wins.
- **kalandra-data (C, semantics):** `stats_by_template` returns real-template hits before
  alias hits, and for `Explicit|Implicit|Sanctified` falls back to the trade2 `sanctum`
  group (relic mods) when the primary kind has no hit. Aliases now also cover other trade
  wordings of the same id, unsuffixed `(Local)`/`(Charm)`/`(Flask)`/... templates, tablet
  `in your Maps`, templatized literal numbers and level-less `Grants Skill:` lines.
- **A (core)**, additive only:
  - New helpers: `item::WEAPON_CLASSES`, `ItemClass::{trade_category, is_weapon}`, `Rarity::{from_header, trade_option}`,
    `matcher::extract_values`, `query::{widen_min, DEFAULT_ENABLED_STATS}`, `ExchangeRates::{rate, convert}`,
    `pricing::{trim_outliers, TRIM_MIN_LISTINGS}`.
  - `parser::looks_like_item` also accepts text starting with `Rarity: Gem` (meta gems have no `Item Class:` line;
    they parse as `ItemClass::Other("Meta Gems")`, category `Gem`, trade category `gem.metagem`).
  - `Item Class: Talismans` is a weapon (`Weapon("Talismans")`, `weapon.talisman`); `Augment` maps to `Rune`, refined to
    `SoulCore` by base name. `Stackable Currency` is refined to `Essence`/`DistilledEmotion`/`Splinter`/`Catalyst`/`Omen`
    by base name.
  - Magic items keep the full name line in `base_type`; `StatMatcher::match_item` moves it to `name` and sets
    `base_type` to the base found by `Catalog::base_in_magic_name`. Negated matches flip the sign of `Modifier::values`.
  - `FilterSet::to_trade_query` always returns the search body; the `Exchange` route body is built by `kalandra-trade`.

### Workstream B (trade), all additive
- Paths verified live 2026-11-05: search `/api/trade2/search/poe2/{league}`, fetch
  `/api/trade2/fetch/{ids}?query={id}&realm=poe2`, exchange `/api/trade2/exchange/poe2/{league}`;
  site links `/trade2/search/poe2/{league}/{id}` and `/trade2/exchange/poe2/{league}/{id}`.
- `ratelimit`: `format_rules`, `format_states`, `Display` for `Rule`/`RuleState`;
  `RateLimiter::{seed, blocked_for, policy_name, try_reserve, reserve_guarded}`, `Reservation`
  (drop guard; releases the slot unless `commit()`ed), `edge_margin` (windows padded by
  `max(period/20, 250ms)`). `policy::DATA` is seeded with `5:10:60`.
- `client`: `TradeClient::{exchange, exchange_url, search_listings, exchange_listings, listings_for, static_id, limiter}`,
  `ListingPage`, `exchange_body`, `fetch_listing`, `exchange_listing`, consts `FETCH_CHUNK`, `MAX_QUEUE_WAIT`,
  module `client::policy`. Requests that would queue longer than `MAX_QUEUE_WAIT` (10s) or hit an active
  restriction return `TradeError::RateLimited` without sending.
- `types`: `ExchangeResponse`, `ExchangeEntry`, `ExchangeListing`, `ExchangeOffer`, `ExchangeSide`.
- New module `rates`: `value_in_base`, `display_price`, `aggregated_summary`, `exchange_name`.
- `PoeNinjaSource`: private `http`/`cache` fields, `with_cache_ttl(self, Duration)`, `rates(league)`.
- `Poe2ScoutSource`: private `http`/cache fields, `with_cache_ttl(base_url, user_agent, ttl)`, `rates(league)`.
- Exchange-route (`QueryRoute::Exchange`) bodies are built in trade (`exchange_body`), not by
  `FilterSet::to_trade_query`; the want id is resolved from `data/static` by `base_type` (or `name`).

### Workstream H (testkit / fake-game / e2e) — all additive
- `kalandra-testkit::fakes`: besides the listed fakes and `fake_platform() -> Platform`, adds
  `FakePlatformBuilder::{new, item_text, advanced_text, copy_delay, window, clipboard, capabilities, build -> (Platform, FakeHandles)}`,
  `FakeHandles { clipboard, input_simulator, window_tracker, hotkeys, input_listener }`, `ClipboardOp`, `OverlayCall`,
  `game_window(Rect)`, `copy_chord()`, `advanced_copy_chord()`, `fake_capabilities()`. Every fake is a cloneable handle
  over shared state. Streams (`subscribe`/`events`/`start`) may be taken once; later calls return `AlreadyStarted`.
- `kalandra-testkit::fixtures`: `mount_trade_fixtures` is `async fn(&MockServer)`. `fixture(name)` reads
  `crates/trade/tests/fixtures/<name>.json`, falling back to testkit's own samples in `crates/testkit/fixtures/`.
  Adds `try_fixture`, `testkit_fixture`, `FixtureError`, `TradeMock` (prices, leagues, total, 429 injection per
  `Endpoint`, `mount`, `serve(addr)`), `MockListing`, `LEAGUES_PATH`/`SEARCH_PREFIX`/`FETCH_PREFIX`.
- `tools/fake-game` CLI adds `--corpus-id <id>`, `--advanced-corpus-id <id>`, `--corpus-dir` (env
  `KALANDRA_CORPUS_DIR`), `--exit-after <secs>`, `--size WxH`; prints `EXIT` on shutdown; `FAKE_GAME_DEBUG=1` logs
  window events to stderr; Cmd+C counts as Ctrl+C (macOS).
- Desktop env vars the e2e suite relies on: `KALANDRA_TRADE_BASE_URL` (overrides `TradeConfig::base_url`, no trailing
  slash, e.g. `http://127.0.0.1:18080`), `KALANDRA_CONFIG_DIR` (already listed), `KALANDRA_TEST_HOOK=<port>`.
- Test hook: every command answers with exactly one line (`ok`, `err <message>`, or JSON for `state`). New command
  `tray <menu-id>` runs the tray menu handler for `open_settings` / `quit` (unknown id -> `err ...`). `state` gains
  optional fields `overlay_pinned: bool`, `settings_visible: bool`, `tray_present: bool`; `last_price` is the
  `PriceSummary` JSON (serde field names) or `null`.
- Web `data-testid`s used by e2e: `price-summary` on the overlay's price block (its text includes the median
  amount), `settings-root` on the settings page root.

### F (linux + platform common) — additive only
- `BackendKind::parse(&str) -> Option<BackendKind>` (the `KALANDRA_BACKEND` grammar) and
  `EnvProbe::is_gnome(&self) -> bool`.
- `copy::looks_like_item(&str) -> bool` (local `Item Class:` check, no core dependency) and
  `copy::copy_chord(advanced: bool) -> Chord`. `copy_item_text` returns `NoItemText` when the
  clipboard changed to non-item text, `Timeout` when nothing arrived.
- `backend::Unsupported(&'static str)`: stand-in implementing every trait (all calls ->
  `PlatformError::Unsupported`, `is_game_focused` -> `true`); backends use it for capabilities
  that are `false`.
- `backend::hook::start_global_hook()` is `pub(crate)` (shared by x11/windows/macos).
- `backend::x11::{overlay, X11Overlay}`; `backend::wayland::{overlay, sigusr2_trigger}`.
  `wayland::overlay` always returns `Unsupported` (layer-shell is applied on the GTK window).
- Wayland `WindowTracker::subscribe` and `InputListener::start` return `Unsupported`; the
  Wayland `HotkeyHost::events()` also yields SIGUSR2 as `PriceCheck`.
- `apps/desktop/src-tauri/src/overlay/linux.rs`: `pre_init() -> BackendKind`,
  `pre_init_with(Option<BackendKind>) -> BackendKind`, `gdk_backend_for(BackendKind)`,
  `set_click_through(&WebviewWindow, bool)`, `anchor_layer(&WebviewWindow, right, top, side)`
  in addition to `prepare`.
- (D windows) Additive: `kalandra_platform::backend::windows` re-exports `raw_window_handle` (`pub use raw_window_handle;`) so `apps/desktop` can call `HasWindowHandle`/`HasDisplayHandle` on its Tauri window and pass the raw handles to `backend::windows::overlay` without a direct dependency or unsafe code. `backend::windows::overlay(RawWindowHandle, RawDisplayHandle)` follows the table above (the doc comment in `backend/mod.rs` still shows the older one-argument form).

- **E macos (additive).** `kalandra_platform::backend::macos` (macOS + `native`) also exposes
  `recheck_permissions() -> Vec<String>`, `open_permission_settings(kind: &str)`,
  `create_with(MacosOptions) -> Result<Platform, PlatformError>`,
  `MacosOptions { copy_modifier, prompt_for_accessibility }` (+ `from_env()`), `CopyModifier
  { Command (default), Control }`, and `COPY_MODIFIER_ENV` (`KALANDRA_MACOS_COPY_MODIFIER`).
  `macos::overlay(...)` is not provided: overlay styling lives in `overlay/macos.rs`. That file
  also exposes `plugin()` (tauri-nspanel, which must be registered) and
  `set_click_through(&WebviewWindow, bool)`. On macOS, `Rect` is in points (logical pixels) with
  a top-left origin.
