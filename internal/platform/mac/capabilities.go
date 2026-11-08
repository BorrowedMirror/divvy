package mac

import "github.com/BorrowedMirror/rust-king-ledger/internal/platform"

// Capability notes.
const (
	noteNoScreenRecording = "Screen Recording not granted: game window matched by process name instead of title"
	noteNoAccessibility   = "Accessibility not granted: copy keystroke disabled, window tracking polls"
	noteNoInputMonitoring = "Input Monitoring not granted: passive keyboard/mouse hook disabled"
)

// capabilities is what the macOS backend can do given perms. GlobalHotkeys and Clipboard are
// left false: Wails provides both and the shell fills them in.
func capabilities(perms permissionState) platform.Capabilities {
	var notes []string
	if !perms.screenRecording {
		notes = append(notes, noteNoScreenRecording)
	}
	if !perms.accessibility {
		notes = append(notes, noteNoAccessibility)
	}
	if !perms.canListen() {
		notes = append(notes, noteNoInputMonitoring)
	}
	return platform.Capabilities{
		// CGWindowList needs no permission; AX only makes it event-driven.
		WindowTracking:      true,
		OverlayAlwaysOnTop:  true,
		OverlayClickThrough: true,
		PassiveHook:         perms.canListen(),
		KeystrokeInjection:  perms.accessibility,
		MissingPermissions:  perms.missing(),
		Notes:               notes,
	}
}
