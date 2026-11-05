# Architecture

RKL reads the item text Path of Exile 2 puts on the clipboard, parses it, builds a trade
query and shows prices in a transparent always-on-top overlay. It never reads game memory or
automates gameplay. The public interfaces are fixed in [`CONTRACTS.md`](../CONTRACTS.md); this
page is the overview.

## Data flow

```text
hotkey (platform::HotkeyHost), tray, or `RKL --trigger` (SIGUSR2/SIGUSR1)
  -> desktop copy flow (platform::copy::copy_item_text: save clipboard, send Ctrl+C (Cmd+C on macOS), poll, restore)
  -> core::parser::parse_item            text -> ParsedItem
  -> core::matcher::StatMatcher          mod lines -> trade stat ids (via data::GameData: Catalog)
  -> core::query::build_filters          ParsedItem -> editable FilterSet
  -> event `item-copied`                 -> web overlay renders item + filters
web edits FilterSet -> ipc `query_price` -> trade::PriceSource::price -> event `price-ready`
```

## Components

| Path | Crate / package | Role |
| --- | --- | --- |
| `crates/core` | `rkl-core` | Pure logic, no I/O: item model, parser, stat matcher, query builder, price summaries. |
| `crates/data` | `rkl-data` | Bundled game data (stats, bases, uniques, statics) as zstd JSON with a sha256 manifest; implements `Catalog`. Refreshed by `cargo xtask data refresh`. |
| `crates/trade` | `rkl-trade` | trade2 HTTP client with header-driven rate limiting, plus poe.ninja / poe2scout price sources behind one `PriceSource` trait. |
| `crates/platform` | `rkl-platform` | OS integration traits (window tracking, hotkeys, input listen/simulate, clipboard, overlay) and backends for Windows, macOS, X11 and Wayland; backend chosen by `detect`. |
| `crates/config` | `rkl-config` | Versioned TOML settings with migration, validation and atomic save. |
| `crates/testkit` | `rkl-testkit` | Item-text corpus, fakes for every platform trait, trade fixtures for wiremock. |
| `apps/desktop/src-tauri` | `rkl-desktop` | Tauri 2 shell: windows (`overlay`, `settings`), tray, IPC commands and events, per-OS overlay styling. |
| `web/` | `rkl-web` | React 19 UI for the overlay, settings and diagnostics; talks to Rust only through generated `src/bindings.ts`. |
| `tools/fake-game` | `fake-game` | A window titled `Path of Exile 2` that serves item text on Ctrl+C, for e2e tests. |
| `e2e/` | | End-to-end suites per OS: WebDriver specs on Windows/Linux, a Rust hook harness (`e2e/macos-harness`) on macOS and, via `e2e/scripts/run-hook-x11.sh`, on X11. |
| `xtask/` | `xtask` | Repo tooling: `gen`, `data`, `coverage`, `lint`, `release`, `fixtures`. |

## Boundaries

- **core is pure.** Everything that touches the network, filesystem or OS lives in another crate,
  which keeps the parser and query logic exhaustively unit-testable (>= 90% line coverage gate).
- **Platform behind traits.** The desktop app only sees `rkl_platform::Platform`; tests use
  `testkit::fakes`. A single global passive hook (`rdev`) is shared by the non-Wayland backends,
  and its callbacks only `try_send` into a channel.
- **Typed IPC.** Commands and events are declared in Rust and exported to TypeScript with
  tauri-specta. The web never calls `invoke()` directly (enforced by eslint), and CI fails if
  `web/src/bindings.ts` is stale.
- **Respectful API use.** The trade client honours the server's rate-limit headers and
  `Retry-After`, caches responses, and sends a descriptive User-Agent.

## Testing layers

| Layer | Tooling | Runs in |
| --- | --- | --- |
| Unit + property tests | nextest, proptest, insta | `ci.yml` (all OSes), with llvm-cov gates |
| HTTP | wiremock with recorded fixtures (`cargo xtask fixtures record`) | `ci.yml` |
| Live API contract | `#[ignore]`d `live_*` tests | `contract.yml` (nightly) |
| Web | Vitest + Testing Library + `mockIPC` | `ci.yml` |
| End to end | tauri-driver / Xvfb / headless sway / macOS harness + fake-game | `e2e.yml` |

See [contributing/ci.md](contributing/ci.md) for every workflow.
