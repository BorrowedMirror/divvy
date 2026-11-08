<p align="center"><img src="assets/brand/rkl-mark.svg" width="128" height="128" alt="Rust King Ledger logo: a three-armed gold swirl ending in crab claws"></p>

# Rust King Ledger (RKL)

An open-source price-check overlay for **Path of Exile 2**. Go + [Wails](https://v3.wails.io) + React.

Hover an item in game, press **Ctrl+D**, and RKL copies the item text, parses it, queries
the official trade API and shows a price panel beside your cursor. **Ctrl+Alt+D** opens a pinned
panel that does not auto-hide; **Ctrl+Space** opens the settings. All three can be rebound.

> RKL only reads what the game puts on the clipboard after the single, user-triggered copy
> keystroke. It never reads game memory, never automates gameplay and never uses OCR.

## Features

- Every PoE2 item class: currency, gems, uniques, rares, magic, waystones, tablets, jewels,
  flasks and charms.
- Editable filters: per-stat min/max, global ±% widening, online-only, league; "open on trade".
- Price sources: official trade2 API (default), poe.ninja, poe2scout.
- Header-driven rate limiting that respects GGG's `X-Rate-Limit-*` and `Retry-After` headers.
- Settings window, tray icon, hotkey rebinding, league auto-detect, diagnostics page.
- Hotkeys are only taken while the game window has focus, so the same keys keep working in
  other apps (Wayland-native excepted: the portal holds them all the time).

## Requirements

The game must run in **windowed** or **borderless windowed** mode. Exclusive fullscreen hides
overlays on Windows and Linux.

### Windows 10/11
Run the per-user setup `.exe` (no administrator rights needed). It installs WebView2 when it is
missing; Windows 11 already has it. Nothing else is required.

### macOS 12+
Install the DMG (universal binary). macOS gates the overlay behind privacy permissions, granted
in System Settings → Privacy & Security to the RKL app (in development: to the terminal
that runs the binary):

| Permission | Why | Without it |
| --- | --- | --- |
| Accessibility (required) | send the copy keystroke; AX events for window tracking | no price check from the hotkey; window tracking falls back to polling |
| Screen & System Audio Recording (recommended) | read other apps' window titles (macOS hides them otherwise); RKL never captures the screen | the game window is matched by process name instead of title |
| Input Monitoring (optional) | passive keyboard hook (Esc to close) | no Esc-to-close (Accessibility alone also enables the hook) |

Global hotkeys and the clipboard need no permission. Without a permission RKL keeps
running with that capability disabled, and **Diagnostics** (tray menu) lists what is missing.
It never crashes or busy-loops waiting for permissions. Screen Recording and Input Monitoring
usually take effect only after a relaunch. Details: [docs/research/macos.md](docs/research/macos.md).

The native macOS client copies items with **Cmd+C**, which RKL sends by default; for the
Windows client under CrossOver/Wine set `RKL_MACOS_COPY_MODIFIER=ctrl`.

### Linux
An x86_64 AppImage is provided.

RKL picks a backend at start-up: the **Settings → Platform** override, else
`RKL_BACKEND`, else auto-detection. Auto-detection chooses X11 whenever `DISPLAY` is set
(XWayland running, GNOME included), Wayland-native when only `WAYLAND_DISPLAY` /
`XDG_SESSION_TYPE=wayland` is present, and X11 otherwise. `GDK_BACKEND` is set to match before
GTK starts.

| Session | Support |
| --- | --- |
| **X11** | Full support. |
| **Wayland + XWayland** (`DISPLAY` set; default on KDE, sway, Hyprland with XWayland enabled) | Full support: RKL runs through XWayland (`GDK_BACKEND=x11`). PoE2 under Proton/Wine also runs through XWayland. |
| **GNOME** (Wayland or X11) | **X11/XWayland by default.** GNOME does not implement `wlr-layer-shell`, so a forced Wayland-native overlay cannot stay above the game. |
| **Wayland-native** (no XWayland, or `RKL_BACKEND=wayland`) | Degraded: overlay anchored to the output via layer-shell (no game-window tracking); hotkeys via the XDG GlobalShortcuts portal; keystroke injection via the virtual-keyboard protocol (wlroots compositors) or `ydotool` (needs `ydotoold`); no passive input hook. |

**Compositor shortcut fallback.** When global hotkeys are unavailable, bind a compositor
shortcut to `rkl --trigger` (price check) or `rkl --trigger --pinned` (pinned
panel). The command hands the request to the already running instance (RKL allows only
one instance per user) and exits; `pkill -USR2 rkl` (price check) and `pkill -USR1
rkl` (pinned) work too.

## Configuration

Settings live in `settings.toml` in the per-OS config directory:

| OS | Settings | Cache | Logs |
| --- | --- | --- | --- |
| Windows | `%APPDATA%\rkl\config\settings.toml` | `%LOCALAPPDATA%\rkl\cache` | `%LOCALAPPDATA%\rkl\data\logs` |
| macOS | `~/Library/Application Support/app.rkl/settings.toml` | `~/Library/Caches/app.rkl` | `~/Library/Application Support/app.rkl/logs` |
| Linux | `$XDG_CONFIG_HOME/rkl/settings.toml` (`~/.config/rkl`) | `$XDG_CACHE_HOME/rkl` (`~/.cache/rkl`) | `$XDG_STATE_HOME/rkl/logs` (`~/.local/state/rkl/logs`) |

The log file is `rkl.log` (rewritten on every start). The Diagnostics page shows the
settings and log paths.

GGG asks API clients to identify themselves; add your contact e-mail in
**Settings → Trade → Contact** and it is appended to the User-Agent.

### Environment variables

| Variable | Effect |
| --- | --- |
| `RKL_CONFIG_DIR` | Put everything under one root (portable mode, tests): `<root>/config/settings.toml`, `<root>/cache`, `<root>/logs`. |
| `RKL_BACKEND` | Force the platform backend: `x11` (or `xwayland`), `wayland`; also `windows`, `macos`, `fake`. Case-insensitive; unknown values are ignored; the Settings → Platform override wins. |
| `RKL_LOG` | Log level: `debug`, `info` (default), `warn` or `error` (`trace` counts as `debug`). |
| `RKL_MACOS_COPY_MODIFIER` | macOS only. Copy-chord modifier: `cmd` (default, native client) or `ctrl` (CrossOver/Wine). |

Development and test-only variables:

| Variable | Read by | Effect |
| --- | --- | --- |
| `RKL_TEST_HOOK` | test-hook builds (`-tags testhook`) | Port of the localhost test hook (protocol in [CONTRACTS.md](CONTRACTS.md#test-hook)). |
| `RKL_TEST_HOOK_PORT_FILE` | test-hook builds | File the hook writes its bound port to (for `RKL_TEST_HOOK=0`). |
| `RKL_TEST_HOOK_TOKEN` | test-hook builds | When set, every hook connection must send `auth <token>` first. |
| `RKL_TRADE_BASE_URL` | test-hook builds | Trade API base URL, no trailing slash (default `https://www.pathofexile.com`); its host is also allowed for *open on trade*. Release builds ignore it. |
| `RKL_NINJA_BASE_URL`, `RKL_SCOUT_BASE_URL` | test-hook builds | poe.ninja and poe2scout base URLs (defaults `https://poe.ninja`, `https://poe2scout.com`). Release builds ignore them. |
| `RKL_CORPUS_DIR` | `e2e/cmd/fake-game` | Corpus directory for `--corpus-id` (same as `--corpus-dir`). |
| `RKL_E2E_*` | e2e tests | Binary paths, how the price check is triggered, where logs are kept; listed in `e2e/doc.go`. |
| `SKIP_BUILD`, `E2E_DISPLAY`, `E2E_XWAYLAND` | `e2e/scripts` | Reuse the built binaries; the Xvfb display (default `:99`); run the clients on XWayland under sway. |
| `RKL_RELEASE_VERSION` | `cmd/release package` | Version to package instead of the `VERSION` file. |
| `RKL_UPDATER_PUBKEY`, `RKL_UPDATER_PRIVATE_KEY` | `cmd/release` | Updater key pair; see [releasing](docs/contributing/releasing.md#updates). |

## Development

```sh
pnpm --dir web install                  # web deps, commitlint, lefthook
pnpm --dir web exec lefthook install    # hooks from .lefthook.yml
pnpm --dir web build                    # the Go binary embeds web/dist
go test -tags gtk3 ./...                # Go tests (Linux; drop -tags gtk3 elsewhere)
pnpm --dir web test                     # web tests
go generate ./...                       # regenerate bindings + commit docs
alias task='go tool -modfile=tools/go.mod wails3 task'
task dev                                # app with hot reload
task check                              # what CI checks on the Go side
```

See [docs/architecture.md](docs/architecture.md) for an overview,
[CONTRACTS.md](CONTRACTS.md) for the interfaces between the parts,
[CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and
[docs/contributing/commit-message.md](docs/contributing/commit-message.md) for commit rules.

## Licence

Licensed under the [Apache License, Version 2.0](LICENSE).
Path of Exile is a trademark of Grinding Gear Games; RKL is not affiliated with or endorsed
by Grinding Gear Games.
