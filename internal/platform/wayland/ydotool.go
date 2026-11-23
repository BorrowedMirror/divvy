package wayland

import (
	"context"
	"os/exec"
	"time"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// commandTimeout bounds a helper command (ydotool, wl-clipboard), so a hung daemon or
// compositor cannot stall a price check.
const commandTimeout = 5 * time.Second

// ydotool is last-resort keystroke injection through the `ydotool` CLI (uinput; needs
// `ydotoold`).
type ydotool struct {
	binary string
}

// detectYdotool finds `ydotool` on PATH; nil when it is not installed.
func detectYdotool() *ydotool {
	bin, err := exec.LookPath("ydotool")
	if err != nil {
		return nil
	}
	return &ydotool{binary: bin}
}

// SendChord implements [platform.InputSimulator].
func (y *ydotool) SendChord(chord platform.Chord) error {
	args, err := keyArgs(chord)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, y.binary, args...).Run(); err != nil {
		return platform.Backend("ydotool: %w", err)
	}
	return nil
}
