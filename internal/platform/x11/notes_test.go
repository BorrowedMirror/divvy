package x11

import (
	"os"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestNotesMentionXWaylandAndGNOME(t *testing.T) {
	probe := platform.EnvProbe{OS: "linux", WaylandDisplay: "wayland-0", Desktop: "GNOME"}
	if notes := sessionNotes(probe); len(notes) != 2 {
		t.Fatalf("notes = %v", notes)
	}
	if notes := sessionNotes(platform.EnvProbe{}); len(notes) != 0 {
		t.Fatalf("notes = %v", notes)
	}
}

func TestGDKBackendMatchesPlatformBackend(t *testing.T) {
	for kind, want := range map[platform.BackendKind]string{
		platform.BackendX11:     "x11",
		platform.BackendFake:    "x11",
		platform.BackendWayland: "wayland",
	} {
		if got := GDKBackendFor(kind); got != want {
			t.Errorf("%s -> %s", kind, got)
		}
	}
	t.Setenv("GDK_BACKEND", "")
	if err := PreInit(platform.BackendWayland); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("GDK_BACKEND"); got != "wayland" {
		t.Fatalf("GDK_BACKEND = %q", got)
	}
}
