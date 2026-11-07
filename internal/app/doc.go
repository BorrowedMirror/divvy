// Package app holds the desktop app's helper modules ported from the Tauri crate
// (apps/desktop/src-tauri/src): the pure app-flow logic (item handling, overlay geometry,
// lookup and trade URLs, chat and stash macros, the waystone mod catalog), remembered filter
// choices, Client.txt reading, tray labels, start at login and the first-run permission report.
// Nothing here depends on Wails; the shell wires these into windows, the tray and IPC.
package app
