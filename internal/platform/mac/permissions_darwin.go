//go:build darwin

package mac

// TCC permission checks. All calls are cheap, non-blocking preflights unless request is used.

import (
	"log/slog"
	"os/exec"
)

// accessibility reports whether this process is a trusted Accessibility client (no prompt).
func accessibility() bool {
	return loadSys() == nil && axIsProcessTrusted()
}

// preflight calls an optional CG access check; a check the OS lacks (before 10.15) means the
// permission does not exist there, i.e. granted.
func preflight(check func() bool) bool {
	return check == nil || check()
}

// currentPermissions is the permission state without prompting. Nothing is granted when the
// frameworks cannot be loaded.
func currentPermissions() PermissionState {
	if loadSys() != nil {
		return PermissionState{}
	}
	return PermissionState{
		Accessibility:   axIsProcessTrusted(),
		ScreenRecording: preflight(cgPreflightScreenCaptureAccess),
		InputMonitoring: preflight(cgPreflightListenEventAccess),
	}
}

// request asks macOS for kind. It shows the system prompt the first time and adds the app to
// the relevant System Settings list; later calls return the current state without UI.
func request(kind string) bool {
	if loadSys() != nil {
		return false
	}
	switch kind {
	case PermAccessibility:
		key, val := kAXTrustedCheckOptionPrompt, kCFBooleanTrue
		options := cfDictionaryCreate(0, &key, &val, 1, kCFTypeDictionaryKeyCallBacks, kCFTypeDictionaryValueCallBacks)
		if options == 0 {
			return axIsProcessTrusted()
		}
		defer cfRelease(options)
		return axIsProcessTrustedWithOptions(options)
	case PermScreenRecording:
		return preflight(cgRequestScreenCaptureAccess)
	case PermInputMonitoring:
		return preflight(cgRequestListenEventAccess)
	}
	return false
}

// TCCProbe is the live macOS permission source for the onboarding view. It has the method set
// of app.PermissionProbe.
type TCCProbe struct{}

// Missing re-queries TCC and returns the permissions still missing (`accessibility`,
// `screen_recording`, `input_monitoring`). Cheap; call when the settings/diagnostics page gains
// focus.
func (TCCProbe) Missing() []string {
	return currentPermissions().Missing()
}

// OpenSettings registers the app for permission kind (shows the one-time system prompt where
// macOS has one) and opens the matching System Settings pane. Unknown kinds are logged and
// ignored.
func (TCCProbe) OpenSettings(kind string) {
	url, ok := settingsURL(kind)
	if !ok {
		slog.Warn("unknown permission kind", "kind", kind)
		return
	}
	request(kind)
	cmd := exec.Command("/usr/bin/open", url)
	if err := cmd.Start(); err != nil {
		slog.Warn("failed to open System Settings", "error", err, "url", url)
		return
	}
	// Reap the child off-thread so callers (UI) never block and no zombie is left.
	go func() { _ = cmd.Wait() }()
}
