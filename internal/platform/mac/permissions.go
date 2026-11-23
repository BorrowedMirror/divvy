package mac

import "github.com/BorrowedMirror/divvy/internal/platform"

// permissionState is the TCC permission state of this process.
type permissionState struct {
	// accessibility is AXIsProcessTrusted: AX observers and synthetic keystrokes (CGEventPost).
	accessibility bool
	// screenRecording is CGPreflightScreenCaptureAccess: other apps' window titles.
	screenRecording bool
	// inputMonitoring is CGPreflightListenEventAccess: listen-only event tap (passive hook).
	inputMonitoring bool
}

// missing returns the missing permissions, in the order the first-run guide asks for them.
func (p permissionState) missing() []string {
	var missing []string
	for _, e := range []struct {
		granted bool
		perm    platform.Permission
	}{
		{p.accessibility, platform.PermissionAccessibility},
		{p.screenRecording, platform.PermissionScreenRecording},
		{p.inputMonitoring, platform.PermissionInputMonitoring},
	} {
		if !e.granted {
			missing = append(missing, string(e.perm))
		}
	}
	return missing
}

// canListen reports whether a listen-only event tap works: it needs Input Monitoring or
// Accessibility.
func (p permissionState) canListen() bool { return p.inputMonitoring || p.accessibility }
