// Package win is Divvy's Windows backend.
//
//   - tracker: SetWinEventHook (foreground + location change) on a dedicated message-loop
//     thread, plus a 1 s rescan timer for the game window appearing or disappearing.
//   - hook: the passive low-level keyboard (and optionally mouse) hook on a message-loop thread
//     of its own.
//   - inject: SendInput chord injection.
//   - overlay: ex-style based no-activate / topmost / click-through styling of an HWND.
//
// Global hotkeys and the clipboard come from Wails (see [platform.Parts]).
//
// The OS-independent parts (key tables, event translation, tracker state, ex-style math) carry
// no build tags so their tests run on every OS; the Win32 calls live in *_windows.go, the user32
// wrappers are generated from the //sys lines in user32_windows.go (go generate).
package win
