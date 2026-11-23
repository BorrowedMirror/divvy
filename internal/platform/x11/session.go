package x11

import "github.com/BorrowedMirror/divvy/internal/platform"

// sessionNotes are diagnostics notes about the session the X11 backend runs in.
func sessionNotes(probe platform.EnvProbe) []string {
	var notes []string
	if probe.WaylandDisplay != "" {
		notes = append(notes,
			"Running through XWayland; the game must also run under XWayland (the default for Proton/Wine)")
	}
	if probe.IsGNOME() {
		notes = append(notes, "GNOME has no layer-shell; using X11/XWayland for the overlay")
	}
	return notes
}
