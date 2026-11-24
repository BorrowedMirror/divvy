package win

import "github.com/BorrowedMirror/divvy/internal/platform"

// capabilities is what the Windows backend can do. Everything is available without extra
// permissions; the notes cover the two real-world failure modes. GlobalHotkeys and Clipboard
// are left for the shell, which provides both from Wails.
func capabilities() platform.Capabilities {
	return platform.Capabilities{
		WindowTracking:      true,
		OverlayAlwaysOnTop:  true,
		OverlayClickThrough: true,
		PassiveHook:         true,
		KeystrokeInjection:  true,
		Notes: []string{
			"If Path of Exile 2 runs as administrator, Divvy must too: Windows (UIPI) blocks " +
				"injected Ctrl+C into elevated windows.",
			"Use windowed or borderless fullscreen; exclusive fullscreen hides the overlay.",
		},
	}
}
