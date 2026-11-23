// Package wayland is the Wayland-native backend (no DISPLAY, or DIVVY_BACKEND=wayland).
//
// What Wayland allows a regular client:
//   - window tracking: impossible (no global window list), so the overlay is anchored to the
//     output instead of the game window;
//   - global hotkeys: the XDG GlobalShortcuts portal, which Wails drives for us (see
//     [GlobalHotkeys] for the capability report), plus the SIGUSR2/SIGUSR1 fallback
//     ([platform.SignalTriggers]);
//   - passive input hook: impossible;
//   - keystrokes: zwp_virtual_keyboard_v1 (wlroots: sway, Hyprland, river...) over a private
//     Wayland connection, else the `ydotool` CLI, else unsupported;
//   - clipboard: wl-clipboard (data-control) when installed, else Wails;
//   - overlay: a gtk-layer-shell surface on the OVERLAY layer (not GNOME: mutter has no
//     layer-shell), see [New] and the Overlay field of the returned parts.
//
// # Overlay and GTK threads
//
// Every GTK call is made through the main-thread dispatcher given as [Options.MainThread]
// (pass Wails' application.InvokeSync). gtk-layer-shell must be initialised before the window
// is realised: call Prepare once, after Wails has created the overlay's GtkWindow (NativeWindow
// is non-nil) and before its first Show. Wails only realises a window in Show, so a window
// created with Hidden: true is still unrealised then; if it was realised anyway, Prepare hides
// and unrealises it first (best effort). A layer surface takes its size from the window's size
// request, not from gtk_window_resize: Prepare pins the request to the window's current size,
// ShowAt to the rect, and the overlay's Resize method ([platform.Resizer]) changes it (Wails'
// SetSize alone does not resize a layer surface).
package wayland
