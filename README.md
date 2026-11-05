# kalandra

An open-source price-check overlay for **Path of Exile 2**, in the spirit of Exiled Exchange 2
and Sidekick. Rust + Tauri 2 + React.

Hover an item in game, press **Ctrl+D**, and kalandra copies the item text, parses it, queries
the official trade API and shows a price panel beside your cursor. **Ctrl+Alt+D** opens a pinned
panel that does not auto-hide.

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
overlays on every OS.

### Windows 10/11
Install the MSI or NSIS installer. WebView2 is bundled with Windows 11 and installed by the
installer on Windows 10. Nothing else is required.

### macOS 13+
Install the DMG (universal binary). On first launch kalandra opens a permission guide:

| Permission | Why | Where |
| --- | --- | --- |
| Accessibility | send the copy keystroke, track the game window | System Settings → Privacy & Security → Accessibility |
| Screen Recording | read the game window title/position (macOS hides window titles otherwise) | System Settings → Privacy & Security → Screen Recording |
| Input Monitoring | optional passive keyboard hook (Esc to close) | System Settings → Privacy & Security → Input Monitoring |

Without a permission kalandra keeps running with that capability disabled; see
**Diagnostics** in the tray menu. It never crashes or busy-loops waiting for permissions.

### Linux
AppImage, `.deb` and `.rpm` are provided. Runtime dependencies: `webkit2gtk-4.1`, `gtk3`,
`libayatana-appindicator` (tray), and for Wayland-native mode `gtk-layer-shell`.

| Session | Support |
| --- | --- |
| **X11** | Full support. |
| **Wayland + XWayland** (default on KDE, sway, Hyprland) | Full support: kalandra runs through XWayland (`GDK_BACKEND=x11` is set automatically). PoE2 under Proton/Wine also runs through XWayland. |
| **GNOME** (Wayland or X11) | **X11/XWayland only.** GNOME does not implement `wlr-layer-shell`, so the Wayland-native overlay is unavailable; kalandra always uses the XWayland path there. |
| **Wayland-native** (`KALANDRA_BACKEND=wayland`, or no XWayland) | Degraded: overlay anchored to the output via layer-shell (no game-window tracking), hotkeys via the XDG GlobalShortcuts portal; if the portal is missing, bind a compositor shortcut to `kalandra --trigger` (or `--trigger --pinned`), which signals the running instance (SIGUSR2/SIGUSR1). Keystroke injection uses libei when available. No passive input hook. |

Override detection with `KALANDRA_BACKEND=x11|wayland` or in **Settings → Platform**.

## Configuration

Settings live in `settings.toml` in the per-OS config directory
(`%APPDATA%\nerv\kalandra\config`, `~/Library/Application Support/au.nerv.kalandra`,
`~/.config/kalandra`). Set `KALANDRA_CONFIG_DIR` to relocate (portable mode).

GGG asks API clients to identify themselves; add your contact e-mail in
**Settings → Trade → Contact** and it is appended to the User-Agent.

## Development

```sh
rustup show                         # installs the pinned toolchain
pnpm install                        # web + commitlint/lefthook
pnpm exec lefthook install          # git hooks
cargo nextest run --workspace       # Rust tests
pnpm --dir web test                 # web tests
cargo xtask gen                     # regenerate bindings + commit docs
cargo xtask coverage --check        # coverage gates
pnpm --dir web dev & cargo run -p kalandra-desktop
```

See [CONTRACTS.md](CONTRACTS.md) for the architecture and every public interface,
[CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and
[docs/contributing/commit-message.md](docs/contributing/commit-message.md) for commit rules.

## Licence

Dual-licensed under [MIT](LICENSE-MIT) or [Apache-2.0](LICENSE-APACHE), at your option.
Path of Exile is a trademark of Grinding Gear Games; kalandra is not affiliated with or endorsed
by Grinding Gear Games.
