// Package mac is Divvy's macOS platform backend, cgo-free over purego.
//
//   - Window tracking: CGWindowListCopyWindowInfo snapshots, made event-driven by an AXObserver
//     on the game's pid running on a dedicated CFRunLoop thread.
//   - Passive hook: a listen-only CGEventTap (Input Monitoring or Accessibility), keyboard only
//     unless Ctrl+wheel stash tab switching asks for the wheel.
//   - Keystroke injection: CGEventPost (Accessibility). The copy chord's Ctrl becomes Cmd by
//     default.
//   - Overlay: extra NSPanel styling over the Wails panel (level, all Spaces, full-screen
//     auxiliary).
//
// Hotkeys and the clipboard come from Wails. Missing permissions never fail [New]; they turn
// capabilities off and are listed in [platform.Capabilities.MissingPermissions].
//
// The pure logic (files without the _darwin suffix) builds on every OS, so it is tested
// everywhere.
package mac
