//go:build darwin

package mac

import (
	"log/slog"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// New builds the macOS backend. Missing permissions never fail it; they turn capabilities off.
// It fails only when the system frameworks cannot be loaded.
//
// ActivateWindow is nil: the Rust backend never refocused the game on macOS (the OS gives focus
// back to the previously active app when the overlay closes).
func New(opts Options) (platform.Parts, error) {
	if err := loadSys(); err != nil {
		return platform.Parts{}, platform.Backend("macos: %v", err)
	}
	if opts.PromptForAccessibility {
		request(PermAccessibility)
	}
	caps := capabilities(currentPermissions())
	slog.Info("macos backend ready", "missing", caps.MissingPermissions)
	return platform.Parts{
		Kind:           platform.BackendMacos,
		Capabilities:   caps,
		WindowTracker:  &windowTracker{},
		InputListener:  inputListener{withMouse: opts.HookMouse},
		InputSimulator: &inputSimulator{copyModifier: opts.CopyModifier},
		Overlay:        newOverlay,
		Cursor:         cursor,
	}, nil
}
