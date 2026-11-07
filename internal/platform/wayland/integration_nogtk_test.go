//go:build linux && waylandtest && !(cgo && gtk3)

package wayland

import (
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func layerShellScenario(t *testing.T, _ platform.Parts) {
	t.Log("NOTE: layer-shell scenario needs cgo and -tags gtk3")
}
