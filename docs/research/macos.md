# macOS backend research and first-run permission guide

Code: `internal/platform/mac/` (the backend, cgo-free over purego) and `internal/app/permissions.go` (the onboarding
report). User-facing summary: [README](../../README.md#macos-12).

> **Status:** the permission model, window tracking, overlay and the first-run Permissions step below are implemented.
> The onboarding view (`#/onboarding`) uses the `get_permissions`, `request_permission` and `recheck_permissions`
> commands.

## Permissions (TCC)

macOS gates everything an overlay needs behind three privacy permissions. They are granted per app bundle (per code
signature). During development that means the terminal or IDE that starts the binary is the app that needs them. Divvy
never crashes or spins when one is missing. It turns the matching capability off, lists the permission in
`Capabilities.missing_permissions`, and keeps working in degraded mode.

| Name in `missing_permissions` | System Settings pane | Check (no prompt) | Request | What breaks without it |
| --- | --- | --- | --- | --- |
| `accessibility` | Privacy & Security > Accessibility | `AXIsProcessTrusted()` | `AXIsProcessTrustedWithOptions({kAXTrustedCheckOptionPrompt: true})` | Synthetic copy keystroke (CGEventPost), so no price check from the hotkey. Window tracking falls back to 250 ms polling instead of AX events. |
| `screen_recording` | Privacy & Security > Screen & System Audio Recording | `CGPreflightScreenCaptureAccess()` | `CGRequestScreenCaptureAccess()` | `kCGWindowName` is stripped from other apps' windows (10.15+). The game window is then matched by owning process name (`Path of Exile 2`, `PathOfExile`, `PathOfExile2`) instead of by title. |
| `input_monitoring` | Privacy & Security > Input Monitoring | `CGPreflightListenEventAccess()` | `CGRequestListenEventAccess()` | Passive hook (listen-only CGEventTap, used for Esc-to-close and Ctrl+scroll). Accessibility alone also satisfies a listen-only tap. |

Global hotkeys and the clipboard (NSPasteboard) come from Wails and need **no** permission.

### Deep links

`request_permission(kind)` (`TCCProbe.OpenSettings`) first calls the matching request function. That registers the app
in the pane's list and shows the system prompt once. It then opens the pane:

- Accessibility: `x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility`
- Screen Recording: `x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture`
- Input Monitoring: `x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent`

These URLs work on macOS 13+ (System Settings) and on older System Preferences.

### Re-checking

TCC changes take effect without a restart for Accessibility, which `AXIsProcessTrusted` reads live. Screen Recording and
Input Monitoring changes are usually only picked up after the app relaunches: macOS itself offers "Quit & Reopen".
`recheck_permissions` (`TCCProbe.Missing`) re-queries all three cheaply. Call it when the settings or diagnostics window
gains focus, never in a loop.

## First-run UX flow

1. On startup the app reads the missing permissions (`Capabilities.MissingPermissions`). If it is empty, nothing is
   shown.
2. Otherwise the settings window opens on a **Permissions** step with one card per missing permission, in the order the
   backend returns them (accessibility, screen_recording, input_monitoring):
   - **Accessibility (required).** "Divvy presses the game's copy shortcut for you when you hit the price-check hotkey.
     macOS requires Accessibility access for that." Button: *Open Accessibility settings*, which calls
     `request_permission("accessibility")`.
   - **Screen Recording (recommended).** "Lets Divvy read window titles so it can find the Path of Exile 2 window
     reliably. Divvy never captures your screen." Button: `request_permission("screen_recording")`. Note: a relaunch is
     needed afterwards.
   - **Input Monitoring (optional).** "Lets Divvy close the overlay with Esc and react to Ctrl+scroll while the game has
     focus." Button: `request_permission("input_monitoring")`.
3. While the view is visible, it calls `recheck_permissions` (every 2 s) and ticks off granted cards. If Screen
   Recording or Input Monitoring was just granted, show *Relaunch Divvy*.
4. *Continue without* is always available. The diagnostics page shows the same cards plus `Capabilities.notes`.

Apps that are not code-signed, or that are re-signed by every dev build, lose their TCC grants whenever the binary
changes. Release builds must be signed with a stable Developer ID.

## Copy chord: Cmd or Ctrl?

On Windows the copy flow is Ctrl+C and Ctrl+Alt+C. The native macOS client follows platform convention, so item copy is
Cmd+C and the advanced copy is Cmd+Option+C. The Windows client running under CrossOver/Wine receives Ctrl+C, because
Wine maps Ctrl to Ctrl. Neither could be verified on hardware from this workstream, so the modifier is configurable:

- Default: Cmd. The backend's `InputSimulator` rewrites every `Ctrl` in the requested chord to `Meta` (Cmd), so the copy
  flow (`platform.CopyItemText`) can keep sending the portable `Ctrl+C`.
- `DIVVY_MACOS_COPY_MODIFIER=ctrl` (`mac.OptionsFromEnv`) sends Ctrl unchanged, for Wine/CrossOver.
- Follow-up: a setting for it, passed through `mac.Options`.

Hotkey defaults (`Ctrl+D`) are not remapped. Cmd+D would clash with many macOS apps, but Ctrl+D is free on macOS, so the
defaults stay as they are.

## Window tracking design

- **Source of truth:** `CGWindowListCopyWindowInfo(OnScreenOnly | ExcludeDesktopElements)`, which lists windows front to
  back. Bounds and pids need no permission. The game is the frontmost layer-0 window, at least 64x64 pt, whose title is
  `Path of Exile 2`, or whose owner name matches when titles are hidden.
- **Focus:** the game counts as focused when its pid owns the frontmost layer-0 window. Our own overlay panel sits at a
  higher layer, so it never counts.
- **Events:** a dedicated, OS-thread-locked goroutine diffs consecutive snapshots into `Found` / `Moved` /
  `FocusChanged` / `Lost`, using non-blocking sends on a bounded channel. With Accessibility it attaches an `AXObserver`
  to the game pid and registers AXWindowMoved, AXWindowResized, AXFocusedWindowChanged, AXMainWindowChanged,
  AXApplicationActivated, AXApplicationDeactivated, AXApplicationHidden, AXApplicationShown, AXWindowMiniaturized,
  AXWindowDeminiaturized and AXUIElementDestroyed. The observer's source wakes
  `CFRunLoopRunInMode(default, 1 s, returnAfterSourceHandled)`. Without AX the thread polls every 250 ms while the game
  is visible and every 1 s while it is not. It never busy-loops: a run loop that returns `Finished` sleeps.
- **Units:** `Rect` is in **points** (logical pixels), with a top-left origin on the primary display, the same space as
  the window positions Wails uses.

## Overlay window

Wails creates the `overlay` window as a non-activating `NSPanel`. `internal/platform/mac/overlay_darwin.go` adds the
rest on top:

- Style mask `NonactivatingPanel`. Clicking the overlay does not activate Divvy, so the game stays the active app. The
  panel becomes key only when a text field needs it.
- Collection behaviour `canJoinAllSpaces | fullScreenAuxiliary | stationary | ignoresCycle`. It shows over a full-screen
  PoE2 Space, follows Space switches, and stays out of Cmd+Tab and Exposé.
- Level `NSStatusWindowLevel` (25), one above the main menu. That is enough to sit over a full-screen app. Screen-saver
  level (1000) would also cover Notification Center and system alerts, which is unnecessary. Wails resets the level to
  floating (3) the first time a hidden always-on-top window becomes key, so the level and shadow are reapplied then.
- `hidesOnDeactivate = false`, `worksWhenModal = true`, no shadow.
- `SetClickThrough(bool)` toggles `ignoresMouseEvents`.

Every AppKit call runs on the main thread.

## Threading notes

- Hotkeys come from Wails. The app only hands each press on to the price-check flow.
- The passive hook is one listen-only `CGEventTap` per process, on its own thread. Its callback only does a non-blocking
  send into a bounded channel; a second start while the first hook lives returns `ErrAlreadyStarted`.
- Window tracking runs on its own OS thread with its own `CFRunLoop`, which the `AXObserver` is attached to.
- purego callbacks are never freed, so the tap and AX callbacks are created once per process.

## Cross-compilation status (verified from Linux)

- `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go vet -tags purego ./...` and the matching golangci-lint run pass
  (`make vet`, `make lint`). The backend itself needs no cgo; the `purego` tag builds Wails without it too, which is
  enough for checking but not for a runnable app.
- Building and packaging the app (`make package:macos`) needs a Mac with the Xcode command line tools.
- The e2e suite (`make e2e:macos`) needs a real Mac with Accessibility and Screen Recording granted to the terminal.
