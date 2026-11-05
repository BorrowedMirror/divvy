# Rust King Ledger (RKL)

An open-source price-check overlay for **Path of Exile 2**, in the spirit of Exiled Exchange 2
and Sidekick. Rust + Tauri 2 + React.

Hover an item in game, press **Ctrl+D**, and RKL copies the item text, parses it, queries
the official trade API and shows a price panel beside your cursor. **Ctrl+Alt+D** opens a pinned
panel that does not auto-hide; **Ctrl+Alt+S** opens the settings. All three can be rebound.

> RKL only reads what the game puts on the clipboard after the single, user-triggered copy
> keystroke. It never reads game memory, never automates gameplay and never uses OCR.

## Features

- Every PoE2 item class: currency, gems, uniques, rares, magic, waystones, tablets, jewels,
  flasks and charms.
- Editable filters: per-stat min/max, global ±% widening, online-only, league; "open on trade".
- Price sources: official trade2 API (default), poe.ninja, poe2scout.
- Header-driven rate limiting that respects GGG's `X-Rate-Limit-*` and `Retry-After` headers.
- Settings window, tray icon, hotkey rebinding, league auto-detect, diagnostics page.

## Requirements

The game must run in **windowed** or **borderless windowed** mode. Exclusive fullscreen hides
overlays on Windows and Linux.

### Windows 10/11
Install the MSI or NSIS installer. WebView2 is bundled with Windows 11 and installed by the
installer on Windows 10. Nothing else is required.

### macOS 13+
Install the DMG (universal binary). macOS gates the overlay behind privacy permissions, granted
in System Settings → Privacy & Security to the RKL app (in development: to the terminal
running `cargo`):

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
AppImage, `.deb` and `.rpm` are provided. Runtime dependencies: `webkit2gtk-4.1`, `gtk3`,
`libayatana-appindicator` (tray), and for Wayland-native mode `gtk-layer-shell`.

RKL picks a backend at start-up: the **Settings → Platform** override, else
`RKL_BACKEND`, else auto-detection. Auto-detection chooses X11 when the desktop is GNOME
(`XDG_CURRENT_DESKTOP` contains `GNOME`) or `DISPLAY` is set (XWayland running), Wayland-native
when only `WAYLAND_DISPLAY` / `XDG_SESSION_TYPE=wayland` is present, and X11 otherwise.
`GDK_BACKEND` is set to match before GTK starts.

| Session | Support |
| --- | --- |
| **X11** | Full support. |
| **Wayland + XWayland** (`DISPLAY` set; default on KDE, sway, Hyprland with XWayland enabled) | Full support: RKL runs through XWayland (`GDK_BACKEND=x11`). PoE2 under Proton/Wine also runs through XWayland. |
| **GNOME** (Wayland or X11) | **X11/XWayland by default.** GNOME does not implement `wlr-layer-shell`, so a forced Wayland-native overlay cannot stay above the game. |
| **Wayland-native** (no XWayland, or `RKL_BACKEND=wayland`) | Degraded: overlay anchored to the output via layer-shell (no game-window tracking); hotkeys via the XDG GlobalShortcuts portal; keystroke injection via the virtual-keyboard protocol (wlroots compositors) or `ydotool` (needs `ydotoold`); no passive input hook. |

**Compositor shortcut fallback.** When global hotkeys are unavailable, bind a compositor
shortcut to `RKL --trigger` (price check) or `RKL --trigger --pinned` (pinned
panel). The command hands the request to the already running instance (RKL allows only
one instance per user) and exits; `pkill -USR2 RKL` (price check) and `pkill -USR1
RKL` (pinned) work too.

## Configuration

Settings live in `settings.toml` in the per-OS config directory (project directories for
`app.rkl`):

| OS | Settings | Cache | Logs |
| --- | --- | --- | --- |
| Windows | `%APPDATA%\nerv\RKL\config\settings.toml` | `%LOCALAPPDATA%\nerv\RKL\cache` | `%LOCALAPPDATA%\nerv\RKL\data\logs` |
| macOS | `~/Library/Application Support/app.rkl/settings.toml` | `~/Library/Caches/app.rkl` | `~/Library/Application Support/app.rkl/logs` |
| Linux | `$XDG_CONFIG_HOME/RKL/settings.toml` (`~/.config/RKL`) | `$XDG_CACHE_HOME/RKL` (`~/.cache/RKL`) | `$XDG_STATE_HOME/RKL/logs` (`~/.local/state/RKL/logs`) |

The log file is `RKL.log` (rewritten on every start). The Diagnostics page shows the
settings and log paths.

GGG asks API clients to identify themselves; add your contact e-mail in
**Settings → Trade → Contact** and it is appended to the User-Agent.

### Environment variables

| Variable | Read by | Effect |
| --- | --- | --- |
| `RKL_CONFIG_DIR` | app | Put everything under one root (portable mode, tests): `<root>/config/settings.toml`, `<root>/cache`, `<root>/logs`. |
| `RKL_BACKEND` | app | Force the platform backend: `x11` (or `xwayland`), `wayland`; also `windows`, `macos`, `fake`. Case-insensitive; unknown values are ignored; the Settings → Platform override wins. |
| `RKL_LOG` | app | `tracing` filter, e.g. `debug` or `rkl_trade=trace` (default `info`). |
| `RKL_MACOS_COPY_MODIFIER` | app (macOS) | Copy-chord modifier: `cmd` (default, native client) or `ctrl` (CrossOver/Wine). |
| `RKL_TRADE_BASE_URL` | app | Trade API base URL, no trailing slash (default `https://www.pathofexile.com`); its host is also allowed for *open on trade*. For mocks/tests. |
| `RKL_NINJA_BASE_URL` | app | poe.ninja base URL (default `https://poe.ninja`). For mocks/tests. |
| `RKL_SCOUT_BASE_URL` | app | poe2scout base URL (default `https://poe2scout.com`). For mocks/tests. |
| `RKL_TEST_HOOK` | app (debug builds, or feature `test-hook`) | Port of the localhost test-hook socket (protocol in [CONTRACTS.md](CONTRACTS.md#appsdesktop-tauri-2)). |

Development and test-only variables:

| Variable | Read by | Effect |
| --- | --- | --- |
| `RKL_UPDATE_BINDINGS` | `bindings` test | `1` rewrites `web/src/bindings.ts` instead of failing when stale (`cargo xtask gen` sets it). |
| `RKL_CORPUS_DIR` | `tools/fake-game` | Corpus directory for `--corpus-id` (same as `--corpus-dir`). |
| `RKL_APP_BIN`, `RKL_FAKE_GAME_BIN` | e2e (wdio, hook harness) | Paths to the `RKL` and `fake-game` binaries (default `target/debug/...`). |
| `RKL_E2E_TRIGGER` | e2e | How the price check is fired. wdio: `auto` (default), `xdotool`, `hook`. Hook harness: `hook` (default), `xdotool`, `signal`. |
| `RKL_E2E_HOTKEY`, `RKL_E2E_HOTKEY_PINNED` | e2e (wdio) | xdotool chords (default `ctrl+d`, `ctrl+alt+d`). |
| `RKL_E2E_ITEM` | e2e | Item text file the fake game serves (default `e2e/fixtures/rare-ring.txt`). |
| `RKL_E2E_MOCK_PORT` | e2e (wdio, mock server) | Port of the mock trade2 server (default `18080`). |
| `RKL_E2E_DRIVER_PORT` | e2e (wdio) | tauri-driver port (default `4444`). |
| `RKL_E2E_KEEP_CONFIG` | e2e (wdio) | Keep the temporary config dir after the run. |
| `RKL_E2E_TIMEOUT_SECS` | e2e (hook harness) | Wait timeout (default `30`). |
| `RKL_E2E_FOCUS_CMD` | e2e (hook harness) | Shell command that focuses the fake game before triggering. |
| `RKL_E2E_IN_SESSION` | e2e scripts | Internal: set once a script runs inside its private display/D-Bus session. |
| `RKL_NATIVE_DRIVER` | e2e (wdio, Linux) | Path to `WebKitWebDriver`. |
| `RKL_TEST_X11_CHILD`, `RKL_TEST_WAYLAND_CHILD` | platform tests | Internal: marks the re-executed child in the Linux display tests. |

## Development

```sh
rustup toolchain install            # installs the pinned toolchain (rust-toolchain.toml)
pnpm install                        # web + e2e deps, commitlint, lefthook
pnpm exec lefthook install          # hooks from .lefthook.yml
cargo nextest run --workspace       # Rust tests
pnpm --dir web test                 # web tests
cargo xtask gen                     # regenerate bindings + commit docs
cargo xtask coverage --check        # coverage gates
pnpm --dir web dev & cargo run -p rkl-desktop
```

See [docs/architecture.md](docs/architecture.md) for an overview,
[CONTRACTS.md](CONTRACTS.md) for every public interface,
[CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and
[docs/contributing/commit-message.md](docs/contributing/commit-message.md) for commit rules.

## Licence

Dual-licensed under [MIT](LICENSE-MIT) or [Apache-2.0](LICENSE-APACHE), at your option.
Path of Exile is a trademark of Grinding Gear Games; RKL is not affiliated with or endorsed
by Grinding Gear Games.
