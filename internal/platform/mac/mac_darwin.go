package mac

import (
	"log/slog"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// New builds the macOS backend. Missing permissions never fail it; they turn capabilities off
// (the onboarding view asks for them through [TCCProbe]). It fails only when the system
// frameworks cannot be loaded.
//
// ActivateWindow is nil: macOS gives focus back to the previously active app when the overlay
// closes, so the game never needs refocusing.
func New(opts Options) (platform.Parts, error) {
	if err := loadSys(); err != nil {
		return platform.Parts{}, platform.Backend("macos: %w", err)
	}
	caps := capabilities(currentPermissions())
	slog.Info("macos backend ready", "missing", caps.MissingPermissions)
	return platform.Parts{
		Kind:           platform.BackendMacOS,
		Capabilities:   caps,
		WindowTracker:  &windowTracker{},
		InputListener:  inputListener{withMouse: opts.HookMouse},
		InputSimulator: &inputSimulator{copyModifier: opts.CopyModifier},
		Overlay:        newOverlay,
		Cursor:         cursor,
	}, nil
}
