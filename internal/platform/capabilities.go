package platform

import "encoding/json"

// Capabilities is the per-capability support report shown on the diagnostics page.
type Capabilities struct {
	WindowTracking      bool `json:"window_tracking"`
	OverlayAlwaysOnTop  bool `json:"overlay_always_on_top"`
	OverlayClickThrough bool `json:"overlay_click_through"`
	GlobalHotkeys       bool `json:"global_hotkeys"`
	PassiveHook         bool `json:"passive_hook"`
	KeystrokeInjection  bool `json:"keystroke_injection"`
	Clipboard           bool `json:"clipboard"`
	// MissingPermissions lists permissions still missing (macOS: `accessibility`,
	// `screen_recording`).
	MissingPermissions []string `json:"missing_permissions"`
	// Notes are human-readable notes, e.g. "Wayland-native: overlay anchored to output".
	Notes []string `json:"notes"`
}

// MarshalJSON writes nil lists as `[]`, never `null`: the frontend expects lists.
func (c Capabilities) MarshalJSON() ([]byte, error) {
	type plain Capabilities
	if c.MissingPermissions == nil {
		c.MissingPermissions = []string{}
	}
	if c.Notes == nil {
		c.Notes = []string{}
	}
	return json.Marshal(plain(c))
}

// Permission is an OS permission RKL may need, as the onboarding view names it. Only macOS has
// any.
type Permission string

const (
	// PermissionAccessibility allows AX observers and synthetic keystrokes (CGEventPost).
	PermissionAccessibility Permission = "accessibility"
	// PermissionScreenRecording reveals other apps' window titles.
	PermissionScreenRecording Permission = "screen_recording"
	// PermissionInputMonitoring allows the listen-only event tap (passive hook).
	PermissionInputMonitoring Permission = "input_monitoring"
)

// SettingsURL is the System Settings deep link for the permission (see
// docs/research/macos.md); ok is false for an unknown permission.
func (p Permission) SettingsURL() (url string, ok bool) {
	const prefix = "x-apple.systempreferences:com.apple.preference.security?"
	switch p {
	case PermissionAccessibility:
		return prefix + "Privacy_Accessibility", true
	case PermissionScreenRecording:
		return prefix + "Privacy_ScreenCapture", true
	case PermissionInputMonitoring:
		return prefix + "Privacy_ListenEvent", true
	}
	return "", false
}
