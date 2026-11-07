// Package app holds the desktop app's helper modules ported from the Tauri crate
// (apps/desktop/src-tauri/src): remembered filter choices, Client.txt reading, tray labels,
// start at login and the first-run permission report. Nothing here depends on Wails; the shell
// wires these into windows, the tray and IPC.
package app
