package win

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestExStyles(t *testing.T) {
	const base uint32 = 0x100
	p := preparedExStyle(base)
	assert.Equal(t, base|wsExNoActivate|wsExToolWindow|wsExTopmost, p)
	assert.Equal(t, p, preparedExStyle(p))

	on, added := clickThroughExStyle(p, true, false)
	assert.Equal(t, p|wsExLayered|wsExTransparent, on)
	assert.True(t, added)
	on2, added2 := clickThroughExStyle(on, true, added)
	assert.Equal(t, on, on2)
	assert.True(t, added2)
	off, added := clickThroughExStyle(on, false, added)
	assert.Equal(t, p, off)
	assert.False(t, added)

	// Already layered by its owner: keep WS_EX_LAYERED when turning click-through off.
	layered := p | wsExLayered
	on, added = clickThroughExStyle(layered, true, false)
	assert.False(t, added)
	off, _ = clickThroughExStyle(on, false, added)
	assert.Equal(t, layered, off)
}

func TestOverlayRequiresWin32Handle(t *testing.T) {
	_, err := hwndFrom(nil)
	require.ErrorIs(t, err, platform.ErrUnsupported)
	var fake byte
	hwnd, err := hwndFrom(unsafe.Pointer(&fake))
	require.NoError(t, err)
	assert.Equal(t, uintptr(unsafe.Pointer(&fake)), hwnd)
}
