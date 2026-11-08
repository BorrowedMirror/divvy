package platformtest

import (
	"slices"
	"sync"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// ClipboardOpKind is the kind of a [ClipboardOp].
type ClipboardOpKind int

// Clipboard operations.
const (
	OpGet ClipboardOpKind = iota + 1
	OpSet
	OpClear
)

// ClipboardOp is one recorded clipboard operation; Text is set for OpSet.
type ClipboardOp struct {
	Kind ClipboardOpKind
	Text string
}

// Clipboard is an in-memory clipboard.
type Clipboard struct {
	mu      sync.Mutex
	text    string
	hasText bool
	history []ClipboardOp
	fail    bool
}

var _ platform.Clipboard = (*Clipboard)(nil)

// NewClipboard returns a clipboard holding text, or an empty one for "".
func NewClipboard(text string) *Clipboard {
	return &Clipboard{text: text, hasText: text != ""}
}

// Peek returns the current contents without recording a Get.
func (c *Clipboard) Peek() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text, c.hasText
}

// Put overwrites the contents without recording a Set (as if another app copied).
func (c *Clipboard) Put(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.text, c.hasText = text, true
}

// PutNothing empties the clipboard without recording a Clear (as if another app cleared it).
func (c *Clipboard) PutNothing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.text, c.hasText = "", false
}

// History is every operation performed through the [platform.Clipboard] interface, in order.
func (c *Clipboard) History() []ClipboardOp {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.history)
}

// SetFailing makes every interface call fail with a backend error until reset.
func (c *Clipboard) SetFailing(fail bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail = fail
}

func (c *Clipboard) check() error {
	if c.fail {
		return platform.Backend("fake clipboard failure")
	}
	return nil
}

// Text implements [platform.Clipboard].
func (c *Clipboard) Text() (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(); err != nil {
		return "", false, err
	}
	c.history = append(c.history, ClipboardOp{Kind: OpGet})
	return c.text, c.hasText, nil
}

// SetText implements [platform.Clipboard].
func (c *Clipboard) SetText(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(); err != nil {
		return err
	}
	c.history = append(c.history, ClipboardOp{Kind: OpSet, Text: text})
	c.text, c.hasText = text, true
	return nil
}

// Clear implements [platform.Clipboard].
func (c *Clipboard) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(); err != nil {
		return err
	}
	c.history = append(c.history, ClipboardOp{Kind: OpClear})
	c.text, c.hasText = "", false
	return nil
}

// HasNonText implements [platform.Clipboard]; the fake only holds text.
func (c *Clipboard) HasNonText() bool { return false }
