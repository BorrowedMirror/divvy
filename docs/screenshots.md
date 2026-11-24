# Screenshots

Every view of Divvy, taken from the browser preview (`pnpm --dir web dev` outside the app, which
fakes the backend with the test fixtures) in Noto Sans, a typical Linux desktop's font. Divvy
uses each system's own UI font, so Windows and macOS show Segoe UI and San Francisco instead.

Retake them all after a web change with `make screenshots` (it needs Chromium), or only some
with `make screenshots NAMES="price-check settings-trade"`. `web/scripts/screenshots.mjs` lists
them.

## Price check

<img src="images/price-check.png" width="420" alt="The price check panel for a rare helmet: its stats and properties as editable trade filters, then the minimum, median and maximum prices and the cheapest listings">

## Settings

### General

<img src="images/settings-general.png" width="672" alt="The General tab: language, theme, start at login, start minimised to tray and automatic update checks">

### Hotkeys

<img src="images/settings-hotkeys.png" width="672" alt="The Hotkeys tab: price check Ctrl+D, pinned price check Ctrl+Alt+D, open settings Ctrl+Space, open on trade site, and advanced item copy">

### In game

<img src="images/settings-in-game.png" width="672" alt="The In game tab: chat command hotkeys such as /hideout on F5, the game log file, stash searches and switching stash tabs with Ctrl and the mouse wheel">

### Trade

<img src="images/settings-trade.png" width="672" alt="The Trade tab: price source, league detection, default sellers, stat range widening, listings to fetch, remembered stat choices, currency comparison and a contact e-mail">

### Overlay

<img src="images/settings-overlay.png" width="672" alt="The Overlay tab: auto-hide delay, opacity, position, panel size, backdrop, item links, where prices and listings go, and listing columns">

### Waystones

<img src="images/settings-waystones.png" width="672" alt="The Waystones tab: waystone mods grouped into player penalties, monsters and area rewards, each flagged danger, warn, good or ignore">

### Platform

<img src="images/settings-platform.png" width="672" alt="The Platform tab: the platform backend, automatic by default">

## Update available

<img src="images/update.png" width="672" alt="The update banner above the settings: Divvy 0.2.0 is available, with Later and Install and restart">

## Diagnostics

<img src="images/diagnostics.png" width="672" alt="Diagnostics: version, OS, backend, game data, league, settings and log files, each capability with a tick or cross, missing permissions and recent errors">

## Setup (macOS)

<img src="images/onboarding.png" width="672" alt="The setup view on macOS: Accessibility, Screen Recording and Input Monitoring, each granted or with a button to open System Settings">

## Ready notice

Shown briefly over the game when Divvy finds the game window.

<img src="images/ready-notice.png" width="300" alt="The ready notice: Divvy, ready and running in the background">
