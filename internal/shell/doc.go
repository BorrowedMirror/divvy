// Package shell builds the Wails application: the two web views (price panel overlay and
// settings), the tray, single-instance handling and the updater, and adapts Wails to the
// interfaces of internal/app (windows, the desktop) and internal/platform (global hotkeys, the
// clipboard). Business logic lives in the other internal packages; this package only wires
// them to Wails.
package shell
