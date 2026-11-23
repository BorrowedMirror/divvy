// Package x11 is Divvy's X11 / XWayland backend, the default on Linux whenever DISPLAY is set.
//
// It provides the game window tracker (EWMH properties and structure events), the passive
// keyboard hook (the RECORD extension on a connection of its own), keystroke injection
// (XTEST, planned with [platform.PlanChord] against the physically held keys) and native
// overlay styling (EWMH state, XFixes input shape, SHAPE rounded corners). Global hotkeys and
// the clipboard come from Wails.
//
// Pure logic (event diffing, key tables, key state, shapes) builds on every OS; everything that
// talks to an X server is Linux only.
package x11
