package win

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCapabilityReport(t *testing.T) {
	c := capabilities()
	assert.True(t, c.WindowTracking && c.OverlayAlwaysOnTop && c.OverlayClickThrough)
	assert.True(t, c.PassiveHook && c.KeystrokeInjection)
	// Hotkeys and the clipboard come from Wails; the shell sets those two.
	assert.False(t, c.GlobalHotkeys || c.Clipboard)
	assert.Empty(t, c.MissingPermissions)
	assert.Len(t, c.Notes, 2)
}
