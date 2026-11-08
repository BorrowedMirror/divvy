// Package app is the desktop app's behaviour behind its windows: the price-check flow (copying
// an item from the game, parsing it, pricing it), the price panel's placement and focus
// handling, global hotkeys, chat and stash macros, settings, the commands the web UI calls
// through the bound [Service], tray labels, the first-run permission report and the e2e test
// hook (test-hook builds only).
//
// Nothing here depends on Wails: the shell implements [Desktop] and [Window] over it and wires
// the flow into the windows, the tray and IPC.
package app
