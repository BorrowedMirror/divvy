//go:build darwin

package mac

import "github.com/BorrowedMirror/rust-king-ledger/internal/platform"

var (
	_ platform.WindowTracker  = (*windowTracker)(nil)
	_ platform.InputListener  = inputListener{}
	_ platform.InputSimulator = (*inputSimulator)(nil)
	_ platform.ChordKeys      = (*cgKeys)(nil)
	_ platform.OverlayWindow  = (*overlay)(nil)
)
