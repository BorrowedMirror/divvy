# macOS backend research and first-run permission guide

Workstream E. Code: `crates/platform/src/backend/macos/`, `apps/desktop/src-tauri/src/overlay/macos.rs`.
Interfaces: [CONTRACTS.md](../../CONTRACTS.md#rkl-platform-cratesplatform); user-facing
summary: [README](../../README.md#macos-13).

> **Status:** the permission model, window tracking and overlay below are implemented. The
> first-run Permissions step is a design: today the app shows missing permissions on the
> Diagnostics page, and `recheck_permissions` / `open_permission_settings` are not yet exposed
> over IPC. The passive hook now uses the shared `backend/hook.rs`.

## Permissions (TCC)

macOS gates everything an overlay needs behind three privacy permissions. They are granted per
app bundle (per code signature). During development that means the terminal or IDE running
`cargo` is the app that needs them. RKL never crashes or spins when one is missing. It
turns the matching capability off, lists the permission in `Capabilities.missing_permissions`,
and keeps working in degraded mode.

| Name in `missing_permissions` | System Settings pane | Check (no prompt) | Request | What breaks without it |
| --- | --- | --- | --- | --- |
| `accessibility` | Privacy & Security > Accessibility | `AXIsProcessTrusted()` | `AXIsProcessTrustedWithOptions({kAXTrustedCheckOptionPrompt: true})` | Synthetic copy keystroke (enigo/CGEventPost), so no price check from the hotkey. Window tracking falls back to 250 ms polling instead of AX events. |
| `screen_recording` | Privacy & Security > Screen & System Audio Recording | `CGPreflightScreenCaptureAccess()` | `CGRequestScreenCaptureAccess()` | `kCGWindowName` is stripped from other apps' windows (10.15+). The game window is then matched by owning process name (`Path of Exile 2`, `PathOfExile`, `PathOfExile2`) instead of by title. |
| `input_monitoring` | Privacy & Security > Input Monitoring | `CGPreflightListenEventAccess()` | `CGRequestListenEventAccess()` | Passive hook (rdev listen-only tap, used for Esc-to-close and Ctrl+scroll). Accessibility alone also satisfies a listen-only tap. |

Global hotkeys use Carbon `RegisterEventHotKey` (through `global-hotkey`), which needs **no**
permission. The clipboard (`arboard`/NSPasteboard) also needs none.

### Deep links

`open_permission_settings(kind)` first calls the matching request function. That registers the
app in the pane's list and shows the system prompt once. It then opens the pane:

- Accessibility: `x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility`
- Screen Recording: `x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture`
- Input Monitoring: `x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent`

These URLs work on macOS 13+ (System Settings) and on older System Preferences.

### Re-checking

TCC changes take effect without a restart for Accessibility, which `AXIsProcessTrusted` reads
live. Screen Recording and Input Monitoring changes are usually only picked up after the app
relaunches: macOS itself offers "Quit & Reopen". `recheck_permissions()` re-queries all three
cheaply. Call it when the settings or diagnostics window gains focus, never in a loop.

## First-run UX flow

1. On startup the desktop app reads `platform.capabilities.missing_permissions`. If it is
   empty, nothing is shown.
2. Otherwise the settings window opens on a **Permissions** step with one card per missing
   permission, in the order the backend returns them (accessibility, screen_recording,
   input_monitoring):
   - **Accessibility (required).** "RKL presses the game's copy shortcut for you when
     you hit the price-check hotkey. macOS requires Accessibility access for that." Button:
     *Open Accessibility settings*, which calls `open_permission_settings("accessibility")`.
   - **Screen Recording (recommended).** "Lets RKL read window titles so it can find
     the Path of Exile 2 window reliably. RKL never captures your screen." Button:
     `open_permission_settings("screen_recording")`. Note: a relaunch is needed afterwards.
   - **Input Monitoring (optional).** "Lets RKL close the overlay with Esc and react to
     Ctrl+scroll while the game has focus." Button:
     `open_permission_settings("input_monitoring")`.
3. When the window regains focus, call `recheck_permissions()` and tick off granted cards. If
   Screen Recording or Input Monitoring was just granted, show *Relaunch RKL*.
4. *Continue without* is always available. The diagnostics page shows the same cards plus
   `Capabilities.notes`.

Apps that are not code-signed, or that are re-signed by every dev build, lose their TCC grants
whenever the binary changes. Release builds must be signed with a stable Developer ID.

## Copy chord: Cmd or Ctrl?

The trade-helper ecosystem (Awakened PoE Trade, Exiled Exchange 2) documents the copy flow as
Ctrl+C and Ctrl+Alt+C on Windows. The native macOS client follows platform convention, so
item copy is Cmd+C and the advanced copy is Cmd+Option+C. The Windows client running under
CrossOver/Wine receives Ctrl+C, because Wine maps Ctrl to Ctrl. Neither could be verified
on hardware from this workstream, so the modifier is configurable:

- Default: `CopyModifier::Command`. The backend's `InputSimulator` rewrites every `Ctrl` in
  the requested chord to `Meta` (Cmd), so `copy.rs` can keep sending the portable `Ctrl+C`.
- `RKL_MACOS_COPY_MODIFIER=ctrl` (or `MacosOptions { copy_modifier: Control }` through
  `create_with`) sends Ctrl unchanged, for Wine/CrossOver.
- Follow-up: once `rkl-config` grows a platform field for it, the lead should pass it
  through `create_with`.

Hotkey defaults (`Ctrl+D`) are not remapped. Cmd+D would clash with many macOS apps, but
Ctrl+D is free on macOS, so the defaults stay as they are.

## Window tracking design

- **Source of truth:** `CGWindowListCopyWindowInfo(OnScreenOnly | ExcludeDesktopElements)`,
  which lists windows front to back. Bounds and pids need no permission. The game is the
  frontmost layer-0 window, at least 64x64 pt, whose title is `Path of Exile 2`, or whose
  owner name matches when titles are hidden.
- **Focus:** the game counts as focused when its pid owns the frontmost layer-0 window. Our own
  overlay panel sits at a higher layer, so it never counts.
- **Events:** a dedicated thread (`rkl-macos-window`) diffs consecutive snapshots into
  `Found` / `Moved` / `FocusChanged` / `Lost`, using `try_send` on a bounded channel. With
  Accessibility it attaches an `AXObserver` to the game pid and registers AXWindowMoved,
  AXWindowResized, AXFocusedWindowChanged, AXMainWindowChanged, AXApplicationActivated,
  AXApplicationDeactivated, AXApplicationHidden, AXApplicationShown, AXWindowMiniaturized,
  AXWindowDeminiaturized and AXUIElementDestroyed. The observer's source wakes
  `CFRunLoopRunInMode(default, 1 s, returnAfterSourceHandled)`. Without AX the thread polls
  every 250 ms while the game is visible and every 1 s while it is not. It never busy-loops:
  a run loop that returns `Finished` sleeps.
- **Units:** `Rect` is in **points** (logical pixels), with a top-left origin on the primary
  display. That matches Tauri `LogicalPosition` / `LogicalSize`.

## Overlay window

`overlay/macos.rs` converts the Tauri `overlay` window into an `NSPanel` subclass with
`tauri-nspanel` 2.1 (crates.io 2.1.1 requires tauri >= 2.8.5, so it works with 2.12):

- Style mask `NonactivatingPanel`. Clicking the overlay does not activate RKL, so the
  game stays the active app. The panel becomes key only when a text field needs it
  (`becomes_key_only_if_needed`).
- Collection behaviour `canJoinAllSpaces | fullScreenAuxiliary | stationary | ignoresCycle`.
  It shows over a full-screen PoE2 Space, follows Space switches, and stays out of Cmd+Tab
  and Exposé.
- Level `NSStatusWindowLevel` (25), one above the main menu. That is enough to sit over a
  full-screen app. Screen-saver level (1000) would also cover Notification Center and
  system alerts, which is unnecessary.
- `hidesOnDeactivate = false`, `worksWhenModal = true`, no shadow.
- `set_click_through(window, bool)` toggles `ignoresMouseEvents`.

The plugin must be registered (`overlay::macos::plugin()`). If it is not, `prepare` returns
an error instead of panicking inside tauri-nspanel's state lookup.

## Threading notes

- `create()` should run on the main thread. Carbon hotkey events reach the application event
  target, which is driven by the AppKit main run loop (Tauri). The handler only does a map
  lookup and `try_send`.
- rdev schedules its event tap on the **main** run loop (`CFRunLoopGetMain`), so its callback
  runs on the main thread. `listen` is called from a helper thread whose own run loop is
  empty, so it returns right after setup. Until F's shared `backend/hook.rs` lands, the macOS
  listener guards with its own process-wide `AtomicBool`.
- enigo is created per `send_chord`. Its `CGEventSource` is not `Send`, and creating one is
  cheap. `open_prompt_to_get_permissions` is off because RKL drives prompts itself.

## Cross-compilation status (verified from Linux)

- `cargo clippy -p rkl-platform --features native --target {aarch64,x86_64}-apple-darwin --all-targets -- -D warnings`
  passes. All platform deps are pure Rust plus framework links, and checking does not link.
- `cargo check -p rkl-desktop --target aarch64-apple-darwin` fails without an Apple SDK,
  in C build scripts that are not ours: `objc2-exception-helper` (pulled in by tauri-nspanel's
  `objc2/exception` feature) and `aws-lc-sys` (rustls via reqwest). The overlay file was checked
  instead in a scratch crate (tauri + tauri-nspanel, with `objc2-exception-helper` patched to a
  build-script-free stub), and clippy passed with `-D warnings`.
- `crates/platform/tests/macos_fake_game.rs` needs a real Mac with Accessibility and Screen
  Recording granted to the terminal. Without them it skips.
