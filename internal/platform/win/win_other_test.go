//go:build !windows

package win

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

func TestNewIsUnsupportedOffWindows(t *testing.T) {
	_, err := New(Options{HookMouse: true})
	assert.ErrorIs(t, err, platform.ErrUnsupported)
}
