package win

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

func TestVKMappingMatchesXSys(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected uint16
	}{
		{
			"KeyC",
			"KeyC",
			'C', // x/sys has no letter constants; VK_C is the ASCII code
		},
		{
			"Escape",
			"Escape",
			windows.VK_ESCAPE,
		},
		{
			"F1",
			"F1",
			windows.VK_F1,
		},
		{
			"Numpad0",
			"Numpad0",
			windows.VK_NUMPAD0,
		},
		{
			"Quote",
			"Quote",
			windows.VK_OEM_7,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := vkForCode(tc.have)
			assert.True(t, ok)
			assert.Equal(t, tc.expected, got)
		})
	}
}
