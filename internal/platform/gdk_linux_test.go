package platform

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGDKBackendMatchesPlatformBackend(t *testing.T) {
	assert.Equal(t, "x11", GDKBackendFor(BackendX11))
	assert.Equal(t, "x11", GDKBackendFor(BackendFake))
	assert.Equal(t, "wayland", GDKBackendFor(BackendWayland))

	t.Setenv("GDK_BACKEND", "")
	require.NoError(t, SetGDKBackend(BackendWayland))
	assert.Equal(t, "wayland", os.Getenv("GDK_BACKEND"))
}
