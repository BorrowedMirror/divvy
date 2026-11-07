// Package mac is RKL's macOS platform backend, cgo-free over purego.
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
// The pure logic (this file, keymap.go, hookmap.go) has no build tags so it is tested on every OS.
package mac

import (
	"math"
	"slices"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// RawWindow is one entry of CGWindowListCopyWindowInfo, reduced to plain values.
type RawWindow struct {
	// Number is kCGWindowNumber (CGWindowID).
	Number uint32
	// PID is kCGWindowOwnerPID.
	PID int32
	// Layer is kCGWindowLayer; 0 is the normal application window layer.
	Layer int64
	// OwnerName is kCGWindowOwnerName (process / app name, needs no permission); nil if absent.
	OwnerName *string
	// Title is kCGWindowName; nil when the process lacks Screen Recording permission (macOS
	// 10.15+ strips it for other apps' windows).
	Title *string
	// X, Y, Width, Height are kCGWindowBounds, in points, global top-left origin.
	X, Y, Width, Height float64
}

// minGameSide: windows smaller than this (points) are helper/tooltip windows, never the game.
const minGameSide = 64.0

// gameOwnerNames are the app/process names the game may run under. Used only when titles are
// hidden (no Screen Recording permission). The native client's bundle is named after the game.
var gameOwnerNames = []string{platform.GameWindowTitle, "PathOfExile", "PathOfExile2"}

// rectFromBounds converts kCGWindowBounds (floating point points) into a [platform.Rect],
// rounding and saturating like Rust's `as` casts (NaN becomes 0).
func rectFromBounds(x, y, width, height float64) platform.Rect {
	clampI32 := func(v float64) int32 {
		if math.IsNaN(v) {
			return 0
		}
		return int32(min(max(math.Round(v), math.MinInt32), math.MaxInt32))
	}
	clampU32 := func(v float64) uint32 {
		if math.IsNaN(v) {
			return 0
		}
		return uint32(min(max(math.Round(v), 0), math.MaxUint32))
	}
	return platform.Rect{X: clampI32(x), Y: clampI32(y), Width: clampU32(width), Height: clampU32(height)}
}

func isGameWindow(w *RawWindow) bool {
	if w.Layer != 0 || w.Width < minGameSide || w.Height < minGameSide {
		return false
	}
	if w.Title != nil {
		return *w.Title == platform.GameWindowTitle
	}
	// Title hidden: fall back to the owning process name.
	return w.OwnerName != nil && slices.Contains(gameOwnerNames, *w.OwnerName)
}

// findGameRaw finds the game window in an on-screen window list ordered front to back.
func findGameRaw(windows []RawWindow) *RawWindow {
	for i := range windows {
		if isGameWindow(&windows[i]) {
			return &windows[i]
		}
	}
	return nil
}

// isFrontmost reports whether pid owns the frontmost normal-layer window, i.e. is the active
// app. Overlays and menus live on higher layers and are ignored.
func isFrontmost(windows []RawWindow, pid int32) bool {
	for _, w := range windows {
		if w.Layer == 0 {
			return w.PID == pid
		}
	}
	return false
}

// findGame returns the game window as a [platform.WindowInfo], with Focused derived from the
// window order.
func findGame(windows []RawWindow) (platform.WindowInfo, bool) {
	w := findGameRaw(windows)
	if w == nil {
		return platform.WindowInfo{}, false
	}
	title := platform.GameWindowTitle
	if w.Title != nil {
		title = *w.Title
	}
	return platform.WindowInfo{
		Title:    title,
		Rect:     rectFromBounds(w.X, w.Y, w.Width, w.Height),
		Focused:  isFrontmost(windows, w.PID),
		NativeID: uint64(w.Number),
	}, true
}

// diff returns the events needed to move a subscriber from prev to next (nil = no window).
func diff(prev, next *platform.WindowInfo) []platform.WindowEvent {
	switch {
	case prev == nil && next == nil:
		return nil
	case prev == nil:
		return []platform.WindowEvent{{Type: platform.WindowFound, Info: *next}}
	case next == nil:
		return []platform.WindowEvent{{Type: platform.WindowLost}}
	case prev.NativeID != next.NativeID:
		return []platform.WindowEvent{{Type: platform.WindowFound, Info: *next}}
	}
	var out []platform.WindowEvent
	if prev.Rect != next.Rect {
		out = append(out, platform.WindowEvent{Type: platform.WindowMoved, Rect: next.Rect})
	}
	if prev.Focused != next.Focused {
		out = append(out, platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: next.Focused})
	}
	return out
}

// Permission kinds, as the onboarding view names them.
const (
	PermAccessibility   = "accessibility"
	PermScreenRecording = "screen_recording"
	PermInputMonitoring = "input_monitoring"
)

// PermissionState is the TCC permission state of this process.
type PermissionState struct {
	// Accessibility is AXIsProcessTrusted: AX observers and synthetic keystrokes (CGEventPost).
	Accessibility bool
	// ScreenRecording is CGPreflightScreenCaptureAccess: other apps' window titles.
	ScreenRecording bool
	// InputMonitoring is CGPreflightListenEventAccess: listen-only event tap (passive hook).
	InputMonitoring bool
}

// Missing returns the names of missing permissions, in the order the first-run guide asks for
// them.
func (p PermissionState) Missing() []string {
	missing := []string{}
	for _, e := range []struct {
		granted bool
		name    string
	}{
		{p.Accessibility, PermAccessibility},
		{p.ScreenRecording, PermScreenRecording},
		{p.InputMonitoring, PermInputMonitoring},
	} {
		if !e.granted {
			missing = append(missing, e.name)
		}
	}
	return missing
}

// CanListen reports whether a listen-only event tap works: it needs Input Monitoring or
// Accessibility.
func (p PermissionState) CanListen() bool { return p.InputMonitoring || p.Accessibility }

// Capability notes.
const (
	noteNoScreenRecording = "Screen Recording not granted: game window matched by process name instead of title"
	noteNoAccessibility   = "Accessibility not granted: copy keystroke disabled, window tracking polls"
	noteNoInputMonitoring = "Input Monitoring not granted: passive keyboard/mouse hook disabled"
)

// capabilities is what the macOS backend can do given perms. GlobalHotkeys and Clipboard are
// left false: Wails provides both and the shell fills them in.
func capabilities(perms PermissionState) platform.Capabilities {
	notes := []string{}
	if !perms.ScreenRecording {
		notes = append(notes, noteNoScreenRecording)
	}
	if !perms.Accessibility {
		notes = append(notes, noteNoAccessibility)
	}
	if !perms.CanListen() {
		notes = append(notes, noteNoInputMonitoring)
	}
	return platform.Capabilities{
		// CGWindowList needs no permission; AX only makes it event-driven.
		WindowTracking:      true,
		OverlayAlwaysOnTop:  true,
		OverlayClickThrough: true,
		PassiveHook:         perms.CanListen(),
		KeystrokeInjection:  perms.Accessibility,
		MissingPermissions:  perms.Missing(),
		Notes:               notes,
	}
}

// settingsURL is the System Settings deep link for a permission kind (see
// docs/research/macos.md); ok is false for unknown kinds.
func settingsURL(kind string) (string, bool) {
	switch kind {
	case PermAccessibility:
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility", true
	case PermScreenRecording:
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture", true
	case PermInputMonitoring:
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent", true
	}
	return "", false
}

// CopyModifier is which modifier the game expects for its copy shortcut on macOS.
//
// The native macOS client follows platform convention (Cmd+C / Cmd+Option+C); the Windows
// client under CrossOver/Wine receives Ctrl+C. Configurable because neither is verified on
// every setup (see docs/research/macos.md).
type CopyModifier int

const (
	// CopyCommand is the default.
	CopyCommand CopyModifier = iota
	CopyControl
)

// ParseCopyModifier parses `cmd`/`command`/`meta` or `ctrl`/`control` (case-insensitive).
func ParseCopyModifier(s string) (CopyModifier, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "cmd", "command", "meta":
		return CopyCommand, true
	case "ctrl", "control":
		return CopyControl, true
	}
	return 0, false
}

// remapChord rewrites a portable chord (written with Ctrl, as on Windows/Linux) for the game on
// macOS.
func remapChord(chord platform.Chord, modifier CopyModifier) platform.Chord {
	mods := make([]platform.KeyModifier, 0, len(chord.Modifiers))
	for _, m := range chord.Modifiers {
		if modifier == CopyCommand && m == platform.Ctrl {
			m = platform.Meta
		}
		if !slices.Contains(mods, m) {
			mods = append(mods, m)
		}
	}
	return platform.Chord{Modifiers: mods, Key: chord.Key}
}

// kCGEventFlagMask* bits (CGEventTypes.h) for the modifiers we model.
const (
	flagShift     uint64 = 0x0002_0000
	flagControl   uint64 = 0x0004_0000
	flagAlternate uint64 = 0x0008_0000
	flagCommand   uint64 = 0x0010_0000
)

// heldFromFlags returns the modifiers held according to CGEventSourceFlagsState bits (Option =
// Alt, Command = Meta).
func heldFromFlags(flags uint64) []platform.KeyModifier {
	var held []platform.KeyModifier
	for _, e := range []struct {
		bit uint64
		mod platform.KeyModifier
	}{
		{flagControl, platform.Ctrl},
		{flagAlternate, platform.Alt},
		{flagShift, platform.Shift},
		{flagCommand, platform.Meta},
	} {
		if flags&e.bit != 0 {
			held = append(held, e.mod)
		}
	}
	return held
}
