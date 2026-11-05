# kalandra

An open-source price-check overlay for **Path of Exile 2**, in the spirit of Exiled Exchange 2
and Sidekick. Rust + Tauri 2 + React.

Hover an item in game, press **Ctrl+D**, and kalandra copies the item text, parses it, queries
the official trade API and shows a price panel beside your cursor. **Ctrl+Alt+D** opens a pinned
panel that does not auto-hide; **Ctrl+Alt+S** opens the settings. All three can be rebound.

> kalandra only reads what the game puts on the clipboard after the single, user-triggered copy
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
in System Settings → Privacy & Security to the kalandra app (in development: to the terminal
running `cargo`):

| Permission | Why | Without it |
| --- | --- | --- |
| Accessibility (required) | send the copy keystroke; AX events for window tracking | no price check from the hotkey; window tracking falls back to polling |
| Screen & System Audio Recording (recommended) | read other apps' window titles (macOS hides them otherwise); kalandra never captures the screen | the game window is matched by process name instead of title |
| Input Monitoring (optional) | passive keyboard hook (Esc to close) | no Esc-to-close (Accessibility alone also enables the hook) |

Global hotkeys and the clipboard need no permission. Without a permission kalandra keeps
running with that capability disabled, and **Diagnostics** (tray menu) lists what is missing.
It never crashes or busy-loops waiting for permissions. Screen Recording and Input Monitoring
usually take effect only after a relaunch. Details: [docs/research/macos.md](docs/research/macos.md).

The native macOS client copies items with **Cmd+C**, which kalandra sends by default; for the
Windows client under CrossOver/Wine set `KALANDRA_MACOS_COPY_MODIFIER=ctrl`.

### Linux
AppImage, `.deb` and `.rpm` are provided. Runtime dependencies: `webkit2gtk-4.1`, `gtk3`,
`libayatana-appindicator` (tray), and for Wayland-native mode `gtk-layer-shell`.

kalandra picks a backend at start-up: the **Settings → Platform** override, else
`KALANDRA_BACKEND`, else auto-detection. Auto-detection chooses X11 when the desktop is GNOME
(`XDG_CURRENT_DESKTOP` contains `GNOME`) or `DISPLAY` is set (XWayland running), Wayland-native
when only `WAYLAND_DISPLAY` / `XDG_SESSION_TYPE=wayland` is present, and X11 otherwise.
`GDK_BACKEND` is set to match before GTK starts.

| Session | Support |
| --- | --- |
| **X11** | Full support. |
| **Wayland + XWayland** (`DISPLAY` set; default on KDE, sway, Hyprland with XWayland enabled) | Full support: kalandra runs through XWayland (`GDK_BACKEND=x11`). PoE2 under Proton/Wine also runs through XWayland. |
| **GNOME** (Wayland or X11) | **X11/XWayland by default.** GNOME does not implement `wlr-layer-shell`, so a forced Wayland-native overlay cannot stay above the game. |
| **Wayland-native** (no XWayland, or `KALANDRA_BACKEND=wayland`) | Degraded: overlay anchored to the output via layer-shell (no game-window tracking); hotkeys via the XDG GlobalShortcuts portal; keystroke injection via the virtual-keyboard protocol (wlroots compositors) or `ydotool` (needs `ydotoold`); no passive input hook. |

**Compositor shortcut fallback.** When global hotkeys are unavailable, bind a compositor
shortcut to `kalandra --trigger` (price check) or `kalandra --trigger --pinned` (pinned
panel). The command reads the running instance's pid from `kalandra.pid` in the cache
directory and sends it `SIGUSR2` or `SIGUSR1` respectively (`pkill -USR2 kalandra` works too).
If you set `KALANDRA_CONFIG_DIR` for the app, set it for the command as well. On the
Wayland-native backend only `SIGUSR2` is handled at the moment, so use `kalandra --trigger`
without `--pinned` there.

## Configuration

Settings live in `settings.toml` in the per-OS config directory (project directories for
`au.nerv.kalandra`):

| OS | Settings | Cache (pid file) | Logs |
| --- | --- | --- | --- |
| Windows | `%APPDATA%\nerv\kalandra\config\settings.toml` | `%LOCALAPPDATA%\nerv\kalandra\cache` | `%LOCALAPPDATA%\nerv\kalandra\data\logs` |
| macOS | `~/Library/Application Support/au.nerv.kalandra/settings.toml` | `~/Library/Caches/au.nerv.kalandra` | `~/Library/Application Support/au.nerv.kalandra/logs` |
| Linux | `$XDG_CONFIG_HOME/kalandra/settings.toml` (`~/.config/kalandra`) | `$XDG_CACHE_HOME/kalandra` (`~/.cache/kalandra`) | `$XDG_STATE_HOME/kalandra/logs` (`~/.local/state/kalandra/logs`) |

The log file is `kalandra.log` (rewritten on every start). The Diagnostics page shows the
settings and log paths.

GGG asks API clients to identify themselves; add your contact e-mail in
**Settings → Trade → Contact** and it is appended to the User-Agent.

### Environment variables

| Variable | Read by | Effect |
| --- | --- | --- |
| `KALANDRA_CONFIG_DIR` | app | Put everything under one root (portable mode, tests): `<root>/config/settings.toml`, `<root>/cache`, `<root>/logs`. |
| `KALANDRA_BACKEND` | app | Force the platform backend: `x11` (or `xwayland`), `wayland`; also `windows`, `macos`, `fake`. Case-insensitive; unknown values are ignored; the Settings → Platform override wins. |
| `KALANDRA_LOG` | app | `tracing` filter, e.g. `debug` or `kalandra_trade=trace` (default `info`). |
| `KALANDRA_MACOS_COPY_MODIFIER` | app (macOS) | Copy-chord modifier: `cmd` (default, native client) or `ctrl` (CrossOver/Wine). |
| `KALANDRA_TRADE_BASE_URL` | app | Trade API base URL, no trailing slash (default `https://www.pathofexile.com`); its host is also allowed for *open on trade*. For mocks/tests. |
| `KALANDRA_NINJA_BASE_URL` | app | poe.ninja base URL (default `https://poe.ninja`). For mocks/tests. |
| `KALANDRA_SCOUT_BASE_URL` | app | poe2scout base URL (default `https://poe2scout.com`). For mocks/tests. |
| `KALANDRA_TEST_HOOK` | app (debug builds, or feature `test-hook`) | Port of the localhost test-hook socket (protocol in [CONTRACTS.md](CONTRACTS.md#appsdesktop-tauri-2)). |

Development and test-only variables:

| Variable | Read by | Effect |
| --- | --- | --- |
| `KALANDRA_UPDATE_BINDINGS` | `bindings` test | `1` rewrites `web/src/bindings.ts` instead of failing when stale (`cargo xtask gen` sets it). |
| `KALANDRA_CORPUS_DIR` | `tools/fake-game` | Corpus directory for `--corpus-id` (same as `--corpus-dir`). |
| `KALANDRA_APP_BIN`, `KALANDRA_FAKE_GAME_BIN` | e2e (wdio, hook harness) | Paths to the `kalandra` and `fake-game` binaries (default `target/debug/...`). |
| `KALANDRA_E2E_TRIGGER` | e2e | How the price check is fired. wdio: `auto` (default), `xdotool`, `hook`. Hook harness: `hook` (default), `xdotool`, `signal`. |
| `KALANDRA_E2E_HOTKEY`, `KALANDRA_E2E_HOTKEY_PINNED` | e2e (wdio) | xdotool chords (default `ctrl+d`, `ctrl+alt+d`). |
| `KALANDRA_E2E_ITEM` | e2e | Item text file the fake game serves (default `e2e/fixtures/rare-ring.txt`). |
| `KALANDRA_E2E_MOCK_PORT` | e2e (wdio, mock server) | Port of the mock trade2 server (default `18080`). |
| `KALANDRA_E2E_DRIVER_PORT` | e2e (wdio) | tauri-driver port (default `4444`). |
| `KALANDRA_E2E_KEEP_CONFIG` | e2e (wdio) | Keep the temporary config dir after the run. |
| `KALANDRA_E2E_TIMEOUT_SECS` | e2e (hook harness) | Wait timeout (default `30`). |
| `KALANDRA_E2E_FOCUS_CMD` | e2e (hook harness) | Shell command that focuses the fake game before triggering. |
| `KALANDRA_E2E_IN_SESSION` | e2e scripts | Internal: set once a script runs inside its private display/D-Bus session. |
| `KALANDRA_NATIVE_DRIVER` | e2e (wdio, Linux) | Path to `WebKitWebDriver`. |
| `KALANDRA_TEST_X11_CHILD`, `KALANDRA_TEST_WAYLAND_CHILD` | platform tests | Internal: marks the re-executed child in the Linux display tests. |

## Development

```sh
rustup toolchain install            # installs the pinned toolchain (rust-toolchain.toml)
pnpm install                        # web + e2e deps, commitlint, lefthook
pnpm exec lefthook install          # hooks from .lefthook.yml
cargo nextest run --workspace       # Rust tests
pnpm --dir web test                 # web tests
cargo xtask gen                     # regenerate bindings + commit docs
cargo xtask coverage --check        # coverage gates
pnpm --dir web dev & cargo run -p kalandra-desktop
```

See [docs/architecture.md](docs/architecture.md) for an overview,
[CONTRACTS.md](CONTRACTS.md) for every public interface,
[CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and
[docs/contributing/commit-message.md](docs/contributing/commit-message.md) for commit rules.

## Licence

Dual-licensed under [MIT](LICENSE-MIT) or [Apache-2.0](LICENSE-APACHE), at your option.
Path of Exile is a trademark of Grinding Gear Games; kalandra is not affiliated with or endorsed
by Grinding Gear Games.
