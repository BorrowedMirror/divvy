package wayland

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// ydotool is last-resort keystroke injection through the `ydotool` CLI (uinput; needs
// `ydotoold`).
type ydotool struct {
	binary string
}

// detectYdotool finds `ydotool` on PATH.
func detectYdotool() (*ydotool, bool) {
	bin, ok := findInPath("ydotool", os.Getenv("PATH"))
	if !ok {
		return nil, false
	}
	return &ydotool{binary: bin}, true
}

// findInPath is the first executable regular file called name in a PATH-style list.
func findInPath(name, path string) (string, bool) {
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// SendChord implements [platform.InputSimulator].
func (y *ydotool) SendChord(chord platform.Chord) error {
	args, err := keyArgs(chord)
	if err != nil {
		return err
	}
	cmd := exec.Command(y.binary, args...)
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return platform.Backend("ydotool exited with %s", exit.ProcessState)
		}
		return platform.Backend("ydotool: %v", err)
	}
	return nil
}
