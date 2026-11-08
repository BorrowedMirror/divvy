package platform

// Clipboard is the system clipboard, text only.
type Clipboard interface {
	// Text returns the clipboard text; ok is false when it holds no text.
	Text() (text string, ok bool, err error)
	SetText(text string) error
	Clear() error
	// HasNonText reports whether the clipboard holds something that is not text (an image),
	// which Text reports as missing and which SetText or Clear would destroy. Backends that
	// cannot tell cheaply return false.
	HasNonText() bool
}
