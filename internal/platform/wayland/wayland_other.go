//go:build !linux

package wayland

import "github.com/BorrowedMirror/rust-king-ledger/internal/platform"

// Options configures [New].
type Options struct {
	Probe      platform.EnvProbe
	MainThread func(func())
}

// New is unsupported off Linux.
func New(Options) (platform.Parts, error) {
	return platform.Parts{}, platform.Unsupported("wayland backend")
}
