package win

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

func TestVKMappingMatchesXSys(t *testing.T) {
	for code, want := range map[string]uint16{
		"KeyC":    'C', // x/sys has no letter constants; VK_C is the ASCII code
		"Escape":  windows.VK_ESCAPE,
		"F1":      windows.VK_F1,
		"Numpad0": windows.VK_NUMPAD0,
		"Quote":   windows.VK_OEM_7,
	} {
		got, ok := vkForCode(code)
		assert.True(t, ok, code)
		assert.Equal(t, want, got, code)
	}
}
