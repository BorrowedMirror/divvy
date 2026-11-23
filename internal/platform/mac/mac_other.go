//go:build !darwin

package mac

import "github.com/BorrowedMirror/divvy/internal/platform"

// New is only available on macOS.
func New(Options) (platform.Parts, error) {
	return platform.Parts{}, platform.Unsupported("macos backend")
}

// TCCProbe reports nothing outside macOS.
type TCCProbe struct{}

// Missing returns nothing outside macOS.
func (TCCProbe) Missing() []string { return nil }

// OpenSettings does nothing outside macOS.
func (TCCProbe) OpenSettings(string) {}
