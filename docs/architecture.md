# Architecture

Divvy reads the item text Path of Exile 2 puts on the clipboard, parses it, builds a trade query and shows prices in a
transparent always-on-top overlay. It never reads game memory or automates gameplay. It is a single Go binary built on
[Wails v3](https://v3.wails.io), with the React UI embedded. The interfaces between the parts are in
[`CONTRACTS.md`](../CONTRACTS.md); this page is the overview.

## Data flow

```text
hotkey (platform.HotkeyHost), tray, or `divvy --trigger` (SIGUSR2/SIGUSR1)
  -> app copy flow (platform.CopyItemText: save clipboard, send Ctrl+C (Cmd+C on macOS), poll, restore)
  -> core.ParseItem                      text -> ParsedItem
  -> core.StatMatcher                    mod lines -> trade stat ids (via data.GameData as core.Catalog)
  -> core.BuildFilters                   ParsedItem -> editable FilterSet
  -> event `item-copied`                 -> web overlay renders item + filters
web edits FilterSet -> command `query_price` -> trade.PriceSource -> event `price-ready`
```

## Components

| Path | Role |
| --- | --- |
| `internal/core` | Pure logic, no I/O: item model, parser, stat matcher, query builder, price summaries. |
| `internal/data` | Bundled game data (stats, bases, uniques, statics, translations) as zstd JSON with a sha256 manifest, embedded in the binary; implements `core.Catalog`. Refreshed by `go run ./cmd/datarefresh refresh`. |
| `internal/trade` | trade2 HTTP client with header-driven rate limiting, plus poe.ninja / poe2scout price sources behind one `PriceSource` interface. |
| `internal/platform` | OS integration interfaces (window tracking, hotkeys, input listen/simulate, clipboard, overlay) and backend detection; backends in `x11`, `wayland`, `win` and `mac` (cgo-free, over purego). Hotkeys and the clipboard come from Wails. |
| `internal/config` | Versioned TOML settings with migration, validation and atomic save; per-OS paths. |
| `internal/ipc` | The wire contract with the web UI: command errors (`ipc.Error`, `IpcError` in TypeScript), request and result types, events and their payloads. |
| `internal/app` | The app's behaviour: price-check flow, state, the bound IPC service, test hook, logging, tray labels, permission report. |
| `internal/shell` | Wails wiring: the `overlay` and `settings` windows, tray, single instance, global hotkeys and clipboard, updater. |
| `internal/platform/platformtest`, `internal/core/coretest` | Test support: in-memory fakes for every platform interface; the item-text corpus (`internal/core/testdata/corpus`). |
| `cmd/divvy` | The app's `main`. |
| `web/` | React 19 UI for the overlay, settings, diagnostics and onboarding; talks to Go only through the generated `src/bindings.ts`. |
| `cmd/bindgen`, `cmd/commitconfig` | Generators behind `go generate ./...`: `web/src/bindings.ts`, and the commitlint config and commit docs. |
| `cmd/datarefresh` | Rebuilds the bundled game data and records the trade fixtures. |
| `cmd/release` | Packaging (AppImage, NSIS installer, DMG), update manifest signing, third-party notices. |
| `e2e/` | End-to-end Go tests (`-tags e2e`), the fake game (`e2e/cmd/fake-game`), a virtual keyboard helper for headless sway (`e2e/cmd/wl-keyboard`) and the session scripts. |
| `tools/game-extract` | Small Rust tool that reads the game's Oodle-compressed bundles for the translation data. |

## Boundaries

- **core is pure.** Everything that touches the network, filesystem or OS lives in another package, which keeps the
  parser and query logic exhaustively unit-testable (90% line coverage target).
- **Platform behind interfaces.** The app only sees `platform.Platform`; tests use the `platformtest` fakes. Each
  backend has at most one passive input hook, and its callbacks only push into a channel.
- **Wails stays in the shell.** `internal/app` and the packages below it do not import Wails, so they are tested without
  a window.
- **Typed IPC.** Commands and events are declared in Go and exported to TypeScript by `cmd/bindgen`. The web never calls
  the Wails runtime's `Call` directly (enforced by Biome), and CI fails if `web/src/bindings.ts` is stale.
- **Respectful API use.** The trade client honours the server's rate-limit headers and `Retry-After`, caches responses,
  and sends a descriptive User-Agent.

## Testing layers

| Layer | Tooling | Runs in |
| --- | --- | --- |
| Unit tests | `go test -race`, JSON goldens in `testdata/` (e.g. `internal/core/testdata/items`), the item corpus | `ci.yml` (all OSes), coverage to Codecov |
| HTTP | `httptest` mock servers over recorded fixtures (`go run ./cmd/datarefresh fixtures`) | `ci.yml` |
| Display | X11 backend tests under Xvfb + openbox | `ci.yml` (Linux) |
| Web | Vitest + Testing Library with an in-memory IPC transport | `ci.yml` |
| End to end | Go tests over the test hook + fake game, under Xvfb, headless sway, Windows and macOS | `e2e.yml` |

See [contributing/ci.md](contributing/ci.md) for every workflow.
