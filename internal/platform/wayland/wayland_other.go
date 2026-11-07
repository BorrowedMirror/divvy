//go:build !linux

package wayland

import (
	"context"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Option configures [New].
type Option func(*struct{})

// WithProbe is a no-op off Linux.
func WithProbe(platform.EnvProbe) Option { return func(*struct{}) {} }

// WithMainThread is a no-op off Linux.
func WithMainThread(func(func())) Option { return func(*struct{}) {} }

// New is unsupported off Linux.
func New(...Option) (platform.Parts, error) {
	return platform.Parts{}, platform.Unsupported("wayland backend")
}

// GlobalHotkeys is false off Linux.
func GlobalHotkeys(context.Context) (bool, []string) { return false, nil }
