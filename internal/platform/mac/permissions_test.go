package mac

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestPermissionsMapToCapabilities(t *testing.T) {
	none := capabilities(permissionState{})
	assert.Equal(t, []string{"accessibility", "screen_recording", "input_monitoring"}, none.MissingPermissions)
	assert.True(t, none.WindowTracking)
	// Wails provides hotkeys and the clipboard; the shell sets those two.
	assert.False(t, none.GlobalHotkeys || none.Clipboard)
	assert.False(t, none.PassiveHook || none.KeystrokeInjection)
	assert.Len(t, none.Notes, 3)

	all := capabilities(permissionState{accessibility: true, screenRecording: true, inputMonitoring: true})
	assert.Empty(t, all.MissingPermissions)
	assert.Empty(t, all.Notes)
	assert.True(t, all.PassiveHook && all.KeystrokeInjection && all.OverlayAlwaysOnTop && all.OverlayClickThrough)

	// Accessibility alone is enough for the listen-only tap.
	ax := capabilities(permissionState{accessibility: true})
	assert.True(t, ax.PassiveHook && ax.KeystrokeInjection)
	assert.Equal(t, []string{"screen_recording", "input_monitoring"}, ax.MissingPermissions)

	im := permissionState{inputMonitoring: true}
	assert.True(t, im.canListen())
	assert.False(t, capabilities(im).KeystrokeInjection)
}

func TestMissingPermissionsHaveSettingsURLs(t *testing.T) {
	for _, kind := range (permissionState{}).missing() {
		u, ok := platform.Permission(kind).SettingsURL()
		assert.True(t, ok && strings.HasPrefix(u, "x-apple.systempreferences:"), kind)
	}
}
