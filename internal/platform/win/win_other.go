//go:build !windows

package win

import "github.com/BorrowedMirror/rust-king-ledger/internal/platform"

// New reports the Windows backend as unsupported on other OSes, so the package (and its
// OS-independent tests) still builds there.
func New(...Option) (platform.Parts, error) {
	return platform.Parts{}, platform.Unsupported("windows backend")
}
