# kalandra contracts

This file is the source of truth for every public interface between the crates, the desktop
shell and the web UI. It describes the code as it is on `master`. Interfaces were fixed in
Phase 1 and extended additively during Phase 2; the [changelog](#changes-since-phase-1) at the
bottom lists what was added.

Changing a contract: prefer the smallest additive change (a new method with a default impl, a
new optional field with `#[serde(default)]`), update the matching table here in the same PR,
and add a line to the changelog.

## Data flow

```
hotkey (platform::HotkeyHost) ──► desktop: copy flow (platform::copy::copy_item_text)
   ──► core::parser::parse_item ──► core::matcher::StatMatcher (data::GameData: Catalog)
   ──► core::query::build_filters ──► events::ItemCopied ──► web overlay
web edits FilterSet ──► ipc::query_price ──► trade::PriceSource::price ──► events::PriceReady
```

Other triggers feed the same flow: the tray menu (*Price check*), Unix signals
(`kalandra --trigger [--pinned]`, see [Desktop](#appsdesktop-tauri-2)) and the debug test hook.

## Crates

### `kalandra-core` (crates/core) — pure, no I/O
| Item | Contract |
| --- | --- |
| `item::{ParsedItem, ItemClass, ItemCategory, Rarity, Modifier, ModKind, AdvancedModInfo, Affix, ValueRange, Property, Requirements, ItemStats, StackSize}` | domain types; serde + specta |
| `item::WEAPON_CLASSES` | `Item Class:` values that map to `ItemClass::Weapon` (includes `Talismans`) |
| `ItemClass::from_header(&str) -> ItemClass` | maps `Item Class:` value; unknown -> `Other(raw)`; weapons -> `Weapon(raw)`; `Augment` -> `Rune` |
| `ItemClass::{category(&self, Rarity) -> ItemCategory, trade_category, is_weapon}` | routing category (unique rarity -> `Unique` unless currency/gem); trade2 category id (e.g. `weapon.talisman`, `gem.metagem`) |
| `Rarity::{from_header, trade_option}` | header value <-> trade2 rarity option |
| `parser::parse_item(&str) -> Result<ParsedItem, ParseError>` | Ctrl+C and Ctrl+Alt+C formats, CRLF or LF |
| `parser::looks_like_item(&str) -> bool` | cheap pre-check; accepts `Item Class:` text and text starting with `Rarity: Gem` (meta gems have no `Item Class:` line and parse as `Other("Meta Gems")`, category `Gem`) |
| `parser::SEPARATOR` | `--------` |
| `catalog::{StatDef, StatAlias, BaseDef, UniqueDef, StaticDef}` | data model |
| `catalog::Catalog` trait | `stats_by_template`, `base`, `base_in_magic_name`, `unique`, `static_item` (semantics under [`kalandra-data`](#kalandra-data-cratesdata)) |
| `matcher::{templatize(&str) -> String, extract_values}` | numbers -> `#`; unsigned value extraction |
| `matcher::StatMatcher::{new, match_item}` | fills `Modifier::stat_id`, returns `Vec<MatchOutcome>`; negated matches flip the sign of `Modifier::values`; for magic items moves the full name line from `base_type` to `name` and sets `base_type` from `Catalog::base_in_magic_name` |
| `query::{QueryOptions, FilterSet, StatFilter, RangeFilter, MiscFilters, OnlineStatus, QueryRoute, TradeQuery}` | editable filters (IPC) and trade2 search body |
| `query::{build_filters(&ParsedItem, &QueryOptions) -> FilterSet, widen_min, DEFAULT_ENABLED_STATS}` | defaults incl. widening |
| `FilterSet::rewiden(&mut self, pct)`, `FilterSet::to_trade_query(&self) -> TradeQuery` | `to_trade_query` always returns the **search** body; the `QueryRoute::Exchange` body is built by `kalandra-trade` |
| `pricing::{Price, Listing, PriceSourceKind, PriceSummary, ExchangeRates::{rate, convert}}` | |
| `pricing::{summarise(kind, total, listings, Option<&ExchangeRates>) -> PriceSummary, trim_outliers, TRIM_MIN_LISTINGS}` | min/median/max, outlier trim |
| `error::{ParseError, CoreError}` | |

Class refinement by base name: `Stackable Currency` -> `Essence` / `DistilledEmotion` /
`Splinter` / `Catalyst` / `Omen`; `Rune` -> `SoulCore`.

### `kalandra-data` (crates/data)
| Item | Contract |
| --- | --- |
| `SCHEMA_VERSION`, `MANIFEST_FILE`, `Manifest`, `AssetEntry`, `Bundle`, `DataError` | asset format: `assets/manifest.json` + `assets/{stats,bases,uniques,statics}.json.zst`; `DataError::Io(String, io::Error)` for missing/unreadable files |
| `decode_asset<T>(&AssetEntry, &[u8]) -> Result<T, DataError>`, `encode_asset`, `sha256_hex`, `parse_manifest` | zstd + sha256 + JSON (and the inverse); manifest JSON + schema check |
| `bundled() -> Result<GameData, DataError>`, `bundled_static() -> &'static GameData` | assets via `include_bytes!`; `bundled_static` is a process-wide cache and `bundled()` a cheap `Arc`-backed clone of it |
| `load_dir(&Path) -> Result<GameData, DataError>` | |
| `write_dir(&Path, &Bundle, &AssetSources, generated_at, game_version) -> Result<Manifest, _>`, `AssetSources` | shared by `cargo xtask data refresh` and tests |
| `GameData::{from_bundle, bundle, manifest}` (+ `pub manifest` field) + `impl Catalog` | indexed lookups (HashMap by template/name) |

`Catalog` semantics:

- `stats_by_template` searches `StatDef::kind` = trade2 stats group id: `Implicit->implicit`,
  `Explicit|Sanctified->explicit`, `Enchant->enchant`, `Rune->rune`, `Crafted->crafted`,
  `Fractured->fractured`, `Desecrated->desecrated`, `Skill->skill`. Real-template hits come
  before alias hits; for `Explicit|Implicit|Sanctified` it falls back to the trade2 `sanctum`
  group (relic mods) when the primary kind has no hit.
- Explicit trade templates are unsigned (`# to maximum Life`). Aliases `+# ...` (negate=false)
  and `-# ...` (negate=true) are generated, plus `#% increased`<->`#% reduced` and
  `#% more`<->`#% less` (negate=true), other trade wordings of the same id, unsuffixed
  `(Local)`/`(Charm)`/`(Flask)`/... templates, tablet `in your Maps`, templatized literal numbers
  and level-less `Grants Skill:` lines. The matcher therefore extracts unsigned numbers and
  applies `negate`.
- `base_in_magic_name` matches whole-word runs only; the longest wins.

### `kalandra-trade` (crates/trade)
| Item | Contract |
| --- | --- |
| `TradeConfig { base_url, user_agent, poesessid, fetch_count, cache_ttl }` (+ `Default`) | |
| `TradeClient::new(TradeConfig, Box<dyn Clock>) -> Result<Self, TradeError>`; `config`, `limiter` | |
| `TradeClient::{leagues, data, search, fetch, exchange}` | `/api/trade2/data/leagues`, `/api/trade2/data/{which}`, `/api/trade2/search/poe2/{league}`, `/api/trade2/fetch/{ids}?query={id}&realm=poe2`, `/api/trade2/exchange/poe2/{league}` (verified live 2026-11-05, see [docs/research/trade2.md](docs/research/trade2.md)) |
| `TradeClient::{trade_url, exchange_url}` | site links `/trade2/search/poe2/{league}/{id}`, `/trade2/exchange/poe2/{league}/{id}` |
| `TradeClient::{search_listings, exchange_listings, listings_for, static_id}`, `ListingPage` | `listings_for(&FilterSet)` routes by `QueryRoute`; the exchange want id is resolved from `data/static` by `base_type` (or `name`) |
| `client::{exchange_body, fetch_listing, exchange_listing, FETCH_CHUNK, MAX_QUEUE_WAIT, policy}` | requests that would queue longer than `MAX_QUEUE_WAIT` (10 s) or hit an active restriction return `TradeError::RateLimited` without sending |
| `PriceSource` trait: `kind`, `supports`, `async price(&PriceRequest)`; `PriceRequest { item, filters }` | implemented by `TradeClient`, `ninja::PoeNinjaSource`, `scout::Poe2ScoutSource` |
| `ninja::PoeNinjaSource::{new(base_url, user_agent), with_cache_ttl(self, Duration), rates(league)}` | |
| `scout::Poe2ScoutSource::{new(base_url, user_agent), with_cache_ttl(base_url, user_agent, ttl), rates(league)}` | |
| `ratelimit::{Clock, SystemClock, Rule, RuleState, parse_rules, parse_states, format_rules, format_states}` | header-driven (`X-Rate-Limit-*`); `Display` for `Rule`/`RuleState` |
| `ratelimit::RateLimiter::{new, reserve, observe, seed, blocked_for, policy_name, try_reserve, reserve_guarded}`, `Reservation`, `edge_margin` | `Retry-After` on 429; `Reservation` is a drop guard that releases the slot unless `commit()`ed; windows padded by `max(period/20, 250ms)`; `policy::DATA` is seeded with `5:10:60` |
| `rates::{value_in_base, display_price, aggregated_summary, exchange_name}` | currency conversion helpers |
| `types::{League, ResultList, SearchResponse, FetchEntry, FetchListing, FetchPrice, FetchAccount, ExchangeResponse, ExchangeEntry, ExchangeListing, ExchangeOffer, ExchangeSide}` | wire types |
| `error::{TradeError, TradeErrorInfo}` | |
| `default_user_agent()` | descriptive UA with contact; settings may append `trade.contact` |

### `kalandra-platform` (crates/platform)
| Item | Contract |
| --- | --- |
| traits `WindowTracker`, `HotkeyHost`, `InputListener`, `InputSimulator`, `Clipboard`, `OverlayWindow` | `mockall::automock` under `cfg(test)`; streams (`subscribe`/`events`/`start`) may be taken once, later calls return `AlreadyStarted` |
| `Platform { kind, capabilities, window_tracker, hotkeys, input_listener, input_simulator, clipboard }` | bundle returned by a backend |
| `types::{Rect, WindowInfo, WindowEvent, KeyModifier, Chord (parse/Display), HotkeyAction, HotkeyEvent, InputEvent, Capabilities, GAME_WINDOW_TITLE}` | `HotkeyAction = PriceCheck \| PriceCheckPinned \| OpenSettings`; `Capabilities { window_tracking, overlay_always_on_top, overlay_click_through, global_hotkeys, passive_hook, keystroke_injection, clipboard, missing_permissions, notes }`. On macOS `Rect` is in points with a top-left origin |
| `detect::{BackendKind, EnvProbe::{from_env, is_gnome}, detect(&EnvProbe, Option<BackendKind>) -> BackendKind}` | settings override > `KALANDRA_BACKEND` > OS. Linux: X11 if GNOME (`XDG_CURRENT_DESKTOP` contains `GNOME`) or `DISPLAY` set; else Wayland-native if `WAYLAND_DISPLAY` set or `XDG_SESSION_TYPE=wayland`; else X11 |
| `BackendKind::parse(&str) -> Option<BackendKind>` | the `KALANDRA_BACKEND` grammar: `x11` \| `xwayland` \| `wayland` \| `windows` \| `macos` \| `fake`, case-insensitive, trimmed; unknown values are ignored |
| `copy::{CopyOptions, copy_item_text(&dyn InputSimulator, &dyn Clipboard, CopyOptions, &dyn Fn(Duration)) -> Result<String, PlatformError>}` | save clipboard, clear, send chord, poll until item text or timeout, restore. `NoItemText` when the clipboard changed to non-item text, `Timeout` when nothing arrived |
| `copy::{looks_like_item(&str) -> bool, copy_chord(advanced: bool) -> Chord}` | local `Item Class:` check (no core dependency); `Ctrl+C` / `Ctrl+Alt+C` |
| `backend::create(BackendKind) -> Result<Platform, PlatformError>` | dispatches to `backend::{windows,macos,x11,wayland}::create()` (feature `native`); `Fake` -> `Unsupported` (fakes live in `kalandra-testkit`) |
| `backend::Unsupported(&'static str)` | stand-in implementing every trait (all calls -> `PlatformError::Unsupported`, `is_game_focused` -> `true`) for capabilities that are `false` |
| `backend::{x11,windows,wayland}::overlay(RawWindowHandle, RawDisplayHandle) -> Result<Box<dyn OverlayWindow>, PlatformError>` | `x11::X11Overlay`; `wayland::overlay` always returns `Unsupported` (layer-shell is applied on the GTK window by the desktop app). macOS has no `overlay` fn: styling lives in `apps/desktop/.../overlay/macos.rs` |
| `backend::windows` re-exports `raw_window_handle` | so `apps/desktop` can pass raw handles without a direct dependency |
| `backend::wayland::sigusr2_trigger() -> Result<Receiver<HotkeyEvent>, _>` | SIGUSR2 -> `PriceCheck`; the Wayland `HotkeyHost::events()` always includes it (portal or not) |
| `backend::macos::{create_with(MacosOptions), MacosOptions { copy_modifier, prompt_for_accessibility } (+ from_env), CopyModifier { Command (default), Control }, COPY_MODIFIER_ENV, recheck_permissions() -> Vec<String>, open_permission_settings(kind: &str)}` | macOS + `native`; `COPY_MODIFIER_ENV = "KALANDRA_MACOS_COPY_MODIFIER"` (`cmd` \| `ctrl`). See [docs/research/macos.md](docs/research/macos.md) |
| `PlatformError` | |

Backend notes:

- The single global passive hook (`rdev`) lives in `backend/hook.rs`
  (`pub(crate) start_global_hook()`) and the Chord -> `global-hotkey` mapping in `backend/ghk.rs`;
  both are shared by the x11, windows and macos backends. Hook callbacks only `try_send` into a
  channel.
- Wayland-native: `WindowTracker::subscribe` and `InputListener::start` return `Unsupported`;
  hotkeys come from the XDG GlobalShortcuts portal when present; keystrokes use the
  `zwp_virtual_keyboard_v1` protocol (wlroots compositors) via enigo, else the `ydotool` CLI;
  libei is deliberately not enabled. The clipboard uses wlr-data-control.

### `kalandra-config` (crates/config)
| Item | Contract |
| --- | --- |
| `Settings { version, league, hotkeys, trade, overlay, ui, platform }` + sections `LeagueSettings, HotkeySettings, TradeSettings, OverlaySettings, OverlaySide, UiSettings, Theme, PlatformSettings { backend_override }` | TOML, `#[serde(default)]` everywhere |
| `SETTINGS_VERSION`, `migrate(toml::Table)`, `validate(&Settings)`, `load(&Paths)`, `save(&Paths, &Settings)` | atomic save; unknown newer version -> `TooNew` |
| `Paths { config_dir, cache_dir, log_dir, settings_file }`, `Paths::resolve()`, `Paths::resolve_with(env: impl Fn(&str) -> Option<String>)`, `Paths::under(root)` | `directories::ProjectDirs::from("au", "nerv", "kalandra")`; `log_dir` = state dir (Linux) or local data dir, `/logs`. A non-empty `KALANDRA_CONFIG_DIR` puts everything under `<root>/{config,cache,logs}` |
| `CONFIG_DIR_ENV` | `"KALANDRA_CONFIG_DIR"` |
| `ConfigError` | |

### `kalandra-testkit` (crates/testkit)
| Item | Contract |
| --- | --- |
| `corpus::{CorpusEntry, corpus_dir, load, get}` | format documented in `src/corpus.rs`; `corpus/index.toml` + `corpus/<class>/<slug>.txt` |
| `fakes::{FakeClipboard, FakeInputSimulator, FakeWindowTracker, FakeHotkeyHost (fire(action)), FakeInputListener, FakeOverlay, fake_platform() -> Platform}` | every fake is a cloneable handle over shared state; `FakeInputSimulator` writes scripted text to a `FakeClipboard` on the copy chord |
| `fakes::{FakePlatformBuilder::{new, item_text, advanced_text, copy_delay, window, clipboard, capabilities, build -> (Platform, FakeHandles)}, FakeHandles { clipboard, input_simulator, window_tracker, hotkeys, input_listener }, ClipboardOp, OverlayCall, game_window(Rect), copy_chord(), advanced_copy_chord(), fake_capabilities()}` | |
| `fixtures::{fixture(name) -> Value, try_fixture, try_fixture_in, testkit_fixture, FixtureError}` | reads `crates/trade/tests/fixtures/<name>.json`, falling back to `crates/testkit/fixtures/` |
| `fixtures::{mount_trade_fixtures(&MockServer) (async), TradeMock, MockListing, Endpoint, LEAGUES_PATH, SEARCH_PREFIX, FETCH_PREFIX}` | `TradeMock`: leagues, prices, total, 429 injection per `Endpoint`, `mount`, `serve(addr)` |

### `apps/desktop` (Tauri 2)
IPC commands (Rust `src/ipc.rs`, exported to `web/src/bindings.ts` via tauri-specta; the web
**never** calls `invoke()` directly — eslint enforces it):

| Command | Signature |
| --- | --- |
| `get_settings` | `() -> Settings` |
| `save_settings` | `(settings: Settings) -> Result<Settings, IpcError>` |
| `get_diagnostics` | `() -> Diagnostics { version, os, backend, capabilities, data_version, league, settings_path, log_path, recent_errors }` |
| `list_leagues` | `() -> Result<Vec<League>, IpcError>` |
| `price_check` | `(request: PriceCheckRequest { text: Option<String>, pinned }) -> Result<PriceCheckResponse { item, filters }, IpcError>` |
| `query_price` | `(filters: FilterSet) -> Result<PriceSummary, IpcError>` |
| `open_trade` | `(url: String) -> Result<(), IpcError>` (only `https://` pathofexile.com, or the `KALANDRA_TRADE_BASE_URL` host) |
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
Tray menu ids: `tray-settings`, `tray-diagnostics`, `tray-price-check`, `tray-quit`.

Per-OS native overlay styling: `apps/desktop/src-tauri/src/overlay/{windows,macos,linux}.rs`,
each `pub fn prepare(&tauri::WebviewWindow) -> Result<(), String>`. In addition:

- `linux.rs`: `pre_init() -> BackendKind`, `pre_init_with(Option<BackendKind>) -> BackendKind`
  (runs `detect` and sets `GDK_BACKEND` to `x11` or `wayland` before GTK starts),
  `gdk_backend_for(BackendKind)`, `set_click_through(&WebviewWindow, bool)`,
  `anchor_layer(&WebviewWindow, right, top, side)` (gtk-layer-shell, Wayland-native only).
- `macos.rs`: `plugin()` (tauri-nspanel; must be registered before `setup`) and
  `set_click_through(&WebviewWindow, bool)`.

CLI and signals (`src/cli.rs`, Unix): `kalandra [--trigger [--pinned]]`. A running instance
writes its pid to `<cache_dir>/kalandra.pid`. `--trigger` reads it and sends `SIGUSR2`
(price check) or, with `--pinned`, `SIGUSR1` (pinned price check), then exits; it exits 1 if
the pid file is missing or corrupt. The app handles SIGUSR1/SIGUSR2 itself on the x11 (and
macOS) backends; on the Wayland-native backend SIGUSR2 arrives through the backend's hotkey
stream instead.

Bindings: `cargo xtask gen` (or `KALANDRA_UPDATE_BINDINGS=1 cargo test -p kalandra-desktop --test bindings`)
regenerates `web/src/bindings.ts`; the `bindings` test fails when stale.

Test hook (`src/testhook.rs`): compiled into debug builds and into release builds with feature
`test-hook`; active only when `KALANDRA_TEST_HOOK=<port>` is set. Binds `127.0.0.1:<port>`;
line protocol, exactly one reply line per command (`ok`, `err <message>`, or JSON):

| Command | Effect / reply |
| --- | --- |
| `trigger 0\|1` | run the price-check flow (unpinned / pinned) |
| `text <json string>` | parse that item text directly, as if copied |
| `state` | JSON `{overlay_visible, overlay_pinned, settings_visible, tray_present, last_item, last_price}`; `last_item` is the base type, `last_price` the `PriceSummary` JSON or `null` |
| `settings` | current `Settings` as JSON |
| `save_settings <json settings>` | validate + persist, like the IPC command |
| `tray open_settings\|open_diagnostics\|price_check\|quit` | run the tray menu handler (`err` if the tray is missing or the id unknown) |
| `quit` | exit the app |

Desktop environment variables are listed in [README.md](README.md#configuration).

### `web/`
React 19 + TS strict + Vite + Tailwind v4 + Base UI (`@base-ui/react`) + Zustand + TanStack Query
+ react-i18next. Route from hash: `overlay | settings | diagnostics` (`src/route.ts`).
Tests: Vitest + RTL + `mockIPC` from `@tauri-apps/api/mocks`, coverage >= 80% lines.
`data-testid`s used by e2e: `price-summary` on the overlay's price block (its text includes
the median amount), `settings-root` on the settings page root.

### `tools/fake-game`
winit window titled `Path of Exile 2`. CLI: `--item <path>` / `--corpus-id <id>` (normal text),
`--advanced-item <path>` / `--advanced-corpus-id <id>` (optional), `--corpus-dir` (env
`KALANDRA_CORPUS_DIR`), `--exit-after <secs>`, `--size WxH` (default `1280x720`). On Ctrl+C
(or Cmd+C) it puts the normal text on the clipboard; on Ctrl+Alt+C the advanced text (or the
normal one). Prints `READY` on stdout once the window is mapped, `COPIED <n>` per copy and
`EXIT` on shutdown. `FAKE_GAME_DEBUG=1` logs window events to stderr.

### `xtask`
| Command | Flags |
| --- | --- |
| `cargo xtask gen` | `--no-bindings`. Regenerates `.commitlintrc.json` and `docs/contributing/commit-message.md` from `.commit-config.toml`, and `web/src/bindings.ts` |
| `cargo xtask data refresh` | `--offline` (use `target/data-cache`) |
| `cargo xtask coverage` | `--check` (enforce gates), `--no-run` (reuse profile data), `--profile <nextest profile>` |
| `cargo xtask lint` | `--no-web`, `--no-tools` (skip cargo-deny/cargo-machete) |
| `cargo xtask release <version>` | `--dry-run`, `--allow-dirty` |
| `cargo xtask fixtures record` | `--league`, `--out`, `--delay-ms` (2000), `--fetch-count` (10), `--base-type` (`Leather Belt`) |

## Gates
- Line coverage (cargo llvm-cov): core, trade, data, config >= 90%; platform >= 70%
  (`#[cfg_attr(coverage_nightly, coverage(off))]` only with a justification comment).
- Web >= 80% lines. Patch coverage >= 80%.
- `cargo clippy --workspace --all-targets -- -D warnings`, `cargo fmt --check`, eslint, tsc.

## Ownership (Phase 2 workstreams)
| WS | Owns |
| --- | --- |
| A core | `crates/core/**`, `crates/testkit/corpus/**` |
| B trade | `crates/trade/**`, `docs/research/trade2.md`, `docs/research/ninja-scout.md` |
| C data | `crates/data/**`, `xtask/src/data/**` (+ the `Data` arm in `xtask/src/main.rs`), `docs/research/data-sources.md` |
| D windows | `crates/platform/src/backend/windows*`, `apps/desktop/src-tauri/src/overlay/windows.rs`, `crates/platform/tests/windows_*.rs` |
| E macos | `crates/platform/src/backend/macos*`, `apps/desktop/src-tauri/src/overlay/macos.rs`, `crates/platform/tests/macos_*.rs`, `docs/research/macos.md` |
| F linux + platform common | `crates/platform/src/{lib,types,detect,copy,error}.rs`, `crates/platform/src/backend/{mod,hook,ghk,x11*,wayland*}`, `crates/platform/Cargo.toml`, `apps/desktop/src-tauri/src/overlay/linux.rs`, `crates/platform/tests/linux_*.rs` |
| G web | `web/**` (except `web/src/bindings.ts`, generated) |
| H testkit/e2e | `crates/testkit/{src,Cargo.toml}`, `tools/fake-game/**`, `e2e/**` |
| I repo/CI | `.github/**`, `xtask/src/{main.rs,coverage.rs,release.rs,lint.rs}`, `.deny.toml`, `.about.toml`, `.about.hbs`, `.cliff.toml`, `docs/**` (except research files above), `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `.renovaterc.json`, `.lefthook.yml`, `.commit-config.toml`, `.codecov.yml` |
| J config | `crates/config/**` |
| Lead | `Cargo.toml`, `apps/desktop/**` (except overlay OS files), `CONTRACTS.md`, `README.md`, merges |

New workspace dependencies go in the root `Cargo.toml` `[workspace.dependencies]` only
(append-only, one line per dep).

## Changes since Phase 1

All additive; the tables above already include them.

- **core (A):** `WEAPON_CLASSES`, `ItemClass::{trade_category, is_weapon}`,
  `Rarity::{from_header, trade_option}`, `matcher::extract_values`,
  `query::{widen_min, DEFAULT_ENABLED_STATS}`, `ExchangeRates::{rate, convert}`,
  `pricing::{trim_outliers, TRIM_MIN_LISTINGS}`; `looks_like_item` accepts meta gems; Talismans
  are weapons; `Augment`/currency refinement by base name; magic-item name handling moved to
  the matcher; `to_trade_query` is search-only.
- **data (C):** `DataError::Io`, `GameData::manifest()`, `bundled_static()`, `MANIFEST_FILE`,
  `sha256_hex`, `encode_asset`, `parse_manifest`, `write_dir`, `AssetSources`; documented
  `Catalog` lookup semantics (kinds, aliases, sanctum fallback, magic-name matching).
- **trade (B):** live-verified paths incl. exchange; `ratelimit` formatting, guarded
  reservations and edge margin; `TradeClient` exchange/listing helpers, `ListingPage`,
  `MAX_QUEUE_WAIT`; exchange wire types; `rates` module; cache TTL constructors and `rates`
  on the poe.ninja / poe2scout sources.
- **platform (D, E, F):** `BackendKind::parse`, `EnvProbe::is_gnome`, `copy::{looks_like_item,
  copy_chord}`, `backend::Unsupported`, shared `hook.rs`/`ghk.rs`, `x11::X11Overlay`,
  `wayland::sigusr2_trigger`, two-argument `overlay(window, display)`, `windows` re-export of
  `raw_window_handle`, macOS `create_with`/`MacosOptions`/`CopyModifier`/permission helpers;
  no `macos::overlay`.
- **config (J):** `Paths::resolve_with`, `CONFIG_DIR_ENV`.
- **testkit / fake-game / e2e (H):** `FakePlatformBuilder` and handles, fixture helpers and
  `TradeMock`; fake-game corpus flags, `--exit-after`, `--size`, `EXIT`; web `data-testid`s.
- **desktop (lead):** `Diagnostics` fields; overlay `linux.rs`/`macos.rs` extras; tray menu;
  `kalandra --trigger [--pinned]` via pid file + SIGUSR2/SIGUSR1 (Phase 1 described it as going
  over the test-hook socket); test hook gained `text`, `settings`, `save_settings`, `tray` and
  extra `state` fields, and is compiled into every debug build; env vars
  `KALANDRA_TRADE_BASE_URL`, `KALANDRA_NINJA_BASE_URL`, `KALANDRA_SCOUT_BASE_URL`,
  `KALANDRA_LOG`.
- **xtask (I):** `gen --no-bindings`, `coverage --no-run/--profile`, `lint --no-web/--no-tools`,
  `release --dry-run/--allow-dirty`, `fixtures record`; root tool configs renamed to dotfiles
  (`.commit-config.toml`, `.cliff.toml`, `.deny.toml`, ...).
