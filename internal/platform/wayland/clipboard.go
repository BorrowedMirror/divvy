//go:build linux

package wayland

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// wlClipboard is the text clipboard through wl-clipboard (`wl-paste`, `wl-copy`), which use the
// compositor's data-control protocol (wlr / ext data-control). Unlike GTK's clipboard (Wails'),
// that works while RKL has no focused window: sway, for one, only tells the focused window
// about clipboard changes, so after the game copies an item GTK would never see it.
type wlClipboard struct {
	paste, copy string // binary paths
	run         func(name string, stdin []byte, args ...string) (stdout []byte, err error)
}

// detectWLClipboard finds wl-paste and wl-copy on PATH; nil when either is missing.
func detectWLClipboard() *wlClipboard {
	paste, err1 := exec.LookPath("wl-paste")
	cp, err2 := exec.LookPath("wl-copy")
	if err1 != nil || err2 != nil {
		return nil
	}
	return &wlClipboard{paste: paste, copy: cp, run: runCmd}
}

func runCmd(name string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, errors.New(msg)
		}
	}
	return out, err
}

// isEmptyClipboard reports wl-paste's "nothing to paste" outcomes, which are not errors.
func isEmptyClipboard(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "nothing is copied") || strings.Contains(msg, "no suitable type")
}

// Text implements [platform.Clipboard]: the clipboard's plain text, if it holds any.
func (c *wlClipboard) Text() (string, bool, error) {
	out, err := c.run(c.paste, nil, "--no-newline", "--type", "text")
	if err != nil {
		if isEmptyClipboard(err) {
			return "", false, nil
		}
		return "", false, platform.Backend("wl-paste: %w", err)
	}
	if len(out) == 0 {
		return "", false, nil
	}
	return string(out), true, nil
}

// SetText implements [platform.Clipboard]. wl-copy keeps serving the text in the background
// after it returns, as a clipboard owner must on Wayland.
func (c *wlClipboard) SetText(text string) error {
	if _, err := c.run(c.copy, []byte(text), "--type", "text/plain;charset=utf-8"); err != nil {
		return platform.Backend("wl-copy: %w", err)
	}
	return nil
}

// Clear implements [platform.Clipboard].
func (c *wlClipboard) Clear() error {
	if _, err := c.run(c.copy, nil, "--clear"); err != nil {
		return platform.Backend("wl-copy --clear: %w", err)
	}
	return nil
}

// HasNonText implements [platform.Clipboard]: something is offered, but not as text (an image),
// which Text reports as missing and SetText/Clear would destroy.
func (c *wlClipboard) HasNonText() bool {
	out, err := c.run(c.paste, nil, "--list-types")
	if err != nil {
		return false
	}
	types := strings.Fields(string(out))
	if len(types) == 0 {
		return false
	}
	for _, t := range types {
		if strings.HasPrefix(t, "text/") || t == "UTF8_STRING" || t == "STRING" || t == "TEXT" {
			return false
		}
	}
	return true
}
