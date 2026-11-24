# Divvy contracts

The interfaces between the parts of Divvy that are not obvious from a single package: the package boundaries, the IPC
between the Go backend and the web UI, the command line, the test hook, the settings and data files, and the generated
files. The Go API of each package is documented in its own doc comments (`go doc ./internal/<package>`); this page does
not repeat it.

Changing a contract: prefer the smallest compatible change (a new optional field, a new command), and update this page
in the same pull request.

## Data flow

```text
hotkey (platform.HotkeyHost), tray, `divvy --trigger` or SIGUSR2/SIGUSR1
  -> app.Flow copy flow (platform.CopyItemText) -> core.ParseItem
  -> core.StatMatcher (data.GameData as core.Catalog) -> core.BuildFilters
  -> event `item-copied` -> web overlay
web edits the FilterSet -> command `query_price` -> trade.PriceSource -> PriceSummary
```

## Packages and boundaries

| Package | Role | May import (internal) |
| --- | --- | --- |
| `internal/core` | Pure domain logic: item model, parser, stat matcher, query builder, price summaries. No I/O. | nothing |
| `internal/data` | Bundled game data (`internal/data/assets`, go:embed), decoded once; implements `core.Catalog`. | `core` |
| `internal/trade` | trade2 client, poe.ninja and poe2scout behind `trade.PriceSource`; header-driven rate limiter; response cache. | `core` |
| `internal/platform` | OS integration interfaces (`WindowTracker`, `HotkeyHost`, `InputListener`, `InputSimulator`, `Clipboard`, `OverlayWindow`), `Capabilities`, backend detection and the copy flow. | nothing |
| `internal/platform/{x11,wayland,win,mac}` | One backend each. | `platform` |
| `internal/config` | `settings.toml` schema, migrations, validation, per-OS paths. | `core`, `platform` |
| `internal/ipc` | Wire contract with the web UI: the command error `ipc.Error` (`IpcError` in TypeScript) and its kinds, request and result types (`PriceCheckRequest`, `Diagnostics`, `PermissionReport`), event names and payloads. | `config`, `core`, `platform`, `trade` |
| `internal/app` | App behaviour: the price-check flow, state, the bound IPC service (`service.go`), test hook, logging, tray labels, permissions. Does not import Wails. | everything above, `web` |
| `internal/shell` | Wails wiring: windows, tray, single instance, hotkeys and clipboard adapters, updater. | `app`, `ipc`, `platform`, `product` |
| `internal/platform/platformtest` | In-memory fakes for every platform interface. Tests only. | `platform` |
| `internal/core/coretest` | The item-text corpus. Tests only. | nothing |
| `web` (Go package) | Embeds `web/dist` and the translation files. | nothing |
| `cmd/divvy` | `main`: picks the backend, builds the shell, wires the flow. | all of the above |

Rules that keep this testable:

- **core is pure.** Anything that touches the network, filesystem or OS lives elsewhere, so the parser and query logic
  can be tested exhaustively.
- **Platform behind interfaces.** The app sees only `platform.Platform`; tests use `platformtest` fakes. One global
  passive input hook per process, and hook callbacks only push into a channel. Missing permissions turn capabilities
  off; they never panic or spin.
- **Wails stays in the shell.** `app` and everything below it build and test without a window.
- **Compatible files.** The settings TOML and the JSON sent to the web UI keep the field names and value shapes of the
  earlier releases, so existing settings load unchanged. `internal/ipc` owns the JSON boundary: lists and maps are never
  `null` on the wire.

## IPC

The bound service is `app.Service` (`internal/app/service.go`): one method per command. The web UI calls the commands
and listens to the events only through the generated `web/src/bindings.ts` (commands are `snake_case`, e.g.
`get_settings`, `price_check`, `query_price`, `open_trade`); Biome forbids calling the Wails runtime's `Call` directly.

Events (`internal/ipc/events.go`): `item-copied`, `price-ready`, `price-failed`, `settings-changed`, `overlay-state`,
`overlay-layout`, `permissions-changed`, `update-available`.

Every command that can fail returns `IpcError { kind, message, retry_after_ms }` (`ipc.Error`; the kinds are the
`ipc.Kind` constants): `parse`, `trade`, `rate_limited` (with `retry_after_ms`), `config`, `platform`, `no_item`,
`busy`, `update`, `internal`.

`open_trade` opens only `https://` pathofexile.com URLs (or, in test-hook builds, the `DIVVY_TRADE_BASE_URL` host). Item
lookups open fixed sites only (`app.LookupURL`).

Windows: `overlay` (`/#/overlay`; transparent, undecorated, always on top, hidden by default) and `settings`
(`/#/settings`; also `#/diagnostics` and `#/onboarding`). The web UI picks its view from the hash (`web/src/route.ts`).

## Command line, single instance and signals

`divvy [--trigger [--pinned]]`. One instance per user on every OS. A second launch focuses the settings window; with
`--trigger` (`--pinned`) it forwards a price check (pinned price check) to the running instance and exits 0, or exits 1
with "no running instance". On Unix the app also handles `SIGUSR2` (price check) and `SIGUSR1` (pinned), so
`pkill -USR2 divvy` works. Price-check triggers from every source are serialised; a trigger during a running copy is
dropped with an `IpcError` of kind `busy`. Test-hook runs (`DIVVY_TEST_HOOK` set) skip the single-instance check.

## Test hook

Compiled only into builds with `-tags testhook` (the e2e builds) and active only when `DIVVY_TEST_HOOK=<port>` is set.
It binds `127.0.0.1:<port>` (port 0 picks a free one; when `DIVVY_TEST_HOOK_PORT_FILE` names a file, the bound port is
written to it) and speaks a line protocol: one command per line, exactly one reply line each (`ok`, `err <message>`, or
JSON). When `DIVVY_TEST_HOOK_TOKEN` is set, the first line of every connection must be `auth <token>`; a line that
starts with an HTTP method closes the connection.

| Command | Effect / reply |
| --- | --- |
| `trigger 0\|1` | run the price-check flow (unpinned / pinned) |
| `text <json string>` | handle that item text directly, as if copied |
| `state` | JSON `{overlay_visible, overlay_pinned, settings_visible, tray_present, last_item, last_price, overlay_layout, ...}` |
| `settings` | current settings as JSON |
| `save_settings <json>` | validate and persist, like the `save_settings` command |
| `tray open_settings\|open_diagnostics\|price_check\|quit` | run that tray item (`err` without a tray) |
| `dom <overlay\|settings> <css selector>` | JSON `{exists, visible, text}` for the first match; read-only |
| `hide` | hide the overlay |
| `quit` | exit the app |

Test-hook builds also honour `DIVVY_TRADE_BASE_URL`, `DIVVY_NINJA_BASE_URL` and `DIVVY_SCOUT_BASE_URL`. Release builds
contain none of this.

## Fake game

`e2e/cmd/fake-game` opens a window titled `Path of Exile 2` (X11, native Wayland, Windows, macOS). On Ctrl+C (or Cmd+C)
it puts the normal item text on the clipboard, on Ctrl+Alt+C the advanced one. Flags: `--item <file>` /
`--corpus-id <id>`, `--advanced-item <file>` / `--advanced-corpus-id <id>`, `--corpus-dir` (env `DIVVY_CORPUS_DIR`),
`--exit-after <secs>`, `--size WxH` (default `1280x720`), `--log-keys`. Stdout: `READY` once the window is mapped,
`COPIED <n>` per copy, `EXIT` on shutdown, and with `--log-keys` `KEY <chord>` / `PASTE <text>`. `FAKE_GAME_DEBUG=1`
logs window events to stderr.

## Data files

| Files | Format | Written by |
| --- | --- | --- |
| `internal/data/assets/manifest.json` + `{stats,bases,uniques,statics,l10n}.json.zst` | zstd JSON; the manifest records schema version, sources and the sha256 of each decompressed file | `go run ./cmd/datarefresh refresh` and `l10n` |
| `internal/core/testdata/corpus/index.toml` + `<class>/<slug>.txt` | item texts as the game copies them | by hand (wrong-price reports) |
| `internal/trade/testdata/fixtures/` | recorded trade2, poe.ninja and poe2scout responses | `go run ./cmd/datarefresh fixtures` (trade2) |
| `settings.toml` | versioned TOML, migrated on load; unknown newer versions are refused | the app |

`core.Catalog` lookup semantics (stat kinds, generated aliases, the sanctum fallback, magic-name matching) are
documented on the interface and on `data.GameData`.

## Generated files

| File | Source | Regenerate | CI check |
| --- | --- | --- | --- |
| `web/src/bindings.ts` | `app.Service` and the `ipc` types | `go generate ./...` | `go run ./cmd/bindgen -check` |
| `.commitlintrc.json`, `docs/contributing/commit-message.md` | `.commit-config.toml` | `go generate ./...` | `go run ./cmd/commitconfig -check` |

## Environment variables

User-facing and development variables are listed in the [README](README.md#environment-variables); the e2e ones in
`e2e/doc.go`.
