package shell

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Clipboard implements platform.Clipboard with Wails' clipboard (it runs on the UI thread).
type Clipboard struct{ app *application.App }

// NewClipboard returns a clipboard on app.
func NewClipboard(a *application.App) Clipboard { return Clipboard{app: a} }

var _ platform.Clipboard = Clipboard{}

// Text returns the clipboard text; ok is false when it holds none.
func (c Clipboard) Text() (string, bool, error) {
	text, ok := c.app.Clipboard.Text()
	if !ok || text == "" {
		return "", false, nil
	}
	return text, true, nil
}

// SetText replaces the clipboard with text.
func (c Clipboard) SetText(text string) error {
	if !c.app.Clipboard.SetText(text) {
		return platform.Backend("clipboard: could not set text")
	}
	return nil
}

// Clear empties the clipboard.
func (c Clipboard) Clear() error { return c.SetText("") }

// HasNonText is false: Wails cannot tell whether the clipboard holds an image.
func (c Clipboard) HasNonText() bool { return false }
