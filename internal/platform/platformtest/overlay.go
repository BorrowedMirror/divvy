package platformtest

import (
	"slices"
	"sync"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// OverlayCallKind is the kind of an [OverlayCall].
type OverlayCallKind int

// Overlay calls.
const (
	CallPrepare OverlayCallKind = iota + 1
	CallShowAt
	CallHide
	CallSetClickThrough
)

// OverlayCall is one recorded [platform.OverlayWindow] call. Rect is set for CallShowAt,
// Enabled for CallSetClickThrough.
type OverlayCall struct {
	Kind    OverlayCallKind
	Rect    platform.Rect
	Enabled bool
}

// Overlay is an overlay styler that records every call.
type Overlay struct {
	mu           sync.Mutex
	calls        []OverlayCall
	visible      bool
	shown        bool // ShowAt was called: rect is set
	rect         platform.Rect
	clickThrough bool
}

var _ platform.OverlayWindow = (*Overlay)(nil)

// NewOverlay returns a hidden overlay.
func NewOverlay() *Overlay { return &Overlay{} }

// Calls returns the recorded calls.
func (o *Overlay) Calls() []OverlayCall {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.calls)
}

// IsVisible reports whether the overlay is shown.
func (o *Overlay) IsVisible() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.visible
}

// Rect is the rect of the last ShowAt; ok is false before the first.
func (o *Overlay) Rect() (rect platform.Rect, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.rect, o.shown
}

// IsClickThrough reports the last SetClickThrough value.
func (o *Overlay) IsClickThrough() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.clickThrough
}

// Prepare implements [platform.OverlayWindow].
func (o *Overlay) Prepare() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallPrepare})
	return nil
}

// ShowAt implements [platform.OverlayWindow].
func (o *Overlay) ShowAt(rect platform.Rect) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallShowAt, Rect: rect})
	o.visible, o.shown, o.rect = true, true, rect
	return nil
}

// Hide implements [platform.OverlayWindow].
func (o *Overlay) Hide() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallHide})
	o.visible = false
	return nil
}

// SetClickThrough implements [platform.OverlayWindow].
func (o *Overlay) SetClickThrough(enabled bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, OverlayCall{Kind: CallSetClickThrough, Enabled: enabled})
	o.clickThrough = enabled
	return nil
}

// RaiseAbove implements [platform.OverlayWindow] as a no-op.
func (*Overlay) RaiseAbove(uint64) error { return nil }

// Activate implements [platform.OverlayWindow] as a no-op.
func (*Overlay) Activate() error { return nil }
