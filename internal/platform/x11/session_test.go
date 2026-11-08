package x11

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestNotesMentionXWaylandAndGNOME(t *testing.T) {
	probe := platform.EnvProbe{OS: "linux", WaylandDisplay: "wayland-0", Desktop: "GNOME"}
	assert.Len(t, sessionNotes(probe), 2)
	assert.Empty(t, sessionNotes(platform.EnvProbe{}))
}
