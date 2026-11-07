// Package wayland is the Wayland-native backend (no DISPLAY, or RKL_BACKEND=wayland).
//
// What Wayland allows a regular client:
//   - window tracking: impossible (no global window list), so the overlay is anchored to the
//     output instead of the game window;
//   - global hotkeys: the XDG GlobalShortcuts portal, which Wails drives for us (see
//     [GlobalHotkeys] for the capability report), plus the SIGUSR2/SIGUSR1 fallback
//     ([SignalTriggers]);
//   - passive input hook: impossible;
//   - keystrokes: zwp_virtual_keyboard_v1 (wlroots: sway, Hyprland, river...) over a private
//     Wayland connection, else the `ydotool` CLI, else unsupported;
//   - clipboard: Wails;
//   - overlay: a gtk-layer-shell surface on the OVERLAY layer (not GNOME: mutter has no
//     layer-shell), see [New] and the Overlay field of the returned parts.
//
// # Signal triggers
//
// SIGUSR2 is a price check and SIGUSR1 a pinned price check, so users without a portal can bind
// `pkill -USR2 rkl` / `pkill -USR1 rkl` (or `rkl --trigger`) in their compositor. The shell
// consumes them with [SignalTriggers], which works on every unix backend: call it at the very
// top of main (an unhandled SIGUSR1 terminates the process) and feed each received
// [platform.HotkeyEvent] into the same path as a Wails hotkey. With this one consumer the Rust
// split (the Wayland platform owned SIGUSR2, the shell owned SIGUSR1 and, elsewhere, SIGUSR2)
// collapses to a single listener; the shell must not install a second one, or each signal is
// delivered twice. Call [MoveJSCSignal] there as well: WebKitGTK's
// JavaScriptCore otherwise takes SIGUSR1 over for its garbage collector and the process crashes
// on the first `pkill -USR1 rkl` (seen under Wails; the Tauri build shared the same WebKitGTK).
//
// # Overlay and GTK threads
//
// Every GTK call is made through the main-thread dispatcher given with [WithMainThread]
// (pass Wails' application.InvokeSync). gtk-layer-shell must be initialised before the window
// is realised: call Prepare once, after Wails has created the overlay's GtkWindow (NativeWindow
// is non-nil) and before its first Show. Wails only realises a window in Show, so a window
// created with Hidden: true is still unrealised then; if it was realised anyway, Prepare hides
// and unrealises it first (best effort, as the Tauri build did). A layer surface takes its size
// from the window's size request, not from gtk_window_resize: Prepare pins the request to the
// window's current size, ShowAt to the rect, and the overlay's Resize method changes it (Wails'
// SetSize alone does not resize a layer surface).
package wayland
