package platform

import (
	"fmt"
	"time"
)

// errorKind classifies a platform [Error].
type errorKind int

const (
	kindUnsupported errorKind = iota + 1
	kindPermissionDenied
	kindInvalidChord
	kindHotkeyConflict
	kindAlreadyStarted
	kindTimeout
	kindNoItemText
	kindBackend
)

// Error is a platform error. Match its kind with errors.Is against the Err* sentinels below.
type Error struct {
	kind errorKind
	// Detail is the capability, permission, chord or hotkey, depending on the kind.
	Detail  string
	Timeout time.Duration
	// Err is the cause of a backend error.
	Err error
}

func (e *Error) Error() string {
	switch e.kind {
	case kindUnsupported:
		return fmt.Sprintf("capability `%s` is not supported on this backend", e.Detail)
	case kindPermissionDenied:
		return fmt.Sprintf("permission `%s` has not been granted", e.Detail)
	case kindInvalidChord:
		return fmt.Sprintf("invalid key chord `%s`", e.Detail)
	case kindHotkeyConflict:
		return "hotkey already registered by another application: " + e.Detail
	case kindAlreadyStarted:
		return "already started"
	case kindTimeout:
		return fmt.Sprintf("timed out after %s", e.Timeout)
	case kindNoItemText:
		return "clipboard did not contain item text"
	case kindBackend:
	}
	if e.Err == nil {
		return "backend error"
	}
	return "backend error: " + e.Err.Error()
}

// Unwrap returns the cause of a backend error.
func (e *Error) Unwrap() error { return e.Err }

// Is matches any error of the same kind, so errors.Is(err, platform.ErrTimeout) works for every
// timeout.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.kind == e.kind
}

// Kind sentinels for errors.Is.
var (
	ErrUnsupported      = &Error{kind: kindUnsupported}
	ErrPermissionDenied = &Error{kind: kindPermissionDenied}
	ErrInvalidChord     = &Error{kind: kindInvalidChord}
	ErrHotkeyConflict   = &Error{kind: kindHotkeyConflict}
	ErrAlreadyStarted   = &Error{kind: kindAlreadyStarted}
	ErrTimeout          = &Error{kind: kindTimeout}
	ErrNoItemText       = &Error{kind: kindNoItemText}
	ErrBackend          = &Error{kind: kindBackend}
)

// Unsupported reports a capability this backend lacks.
func Unsupported(capability string) error { return &Error{kind: kindUnsupported, Detail: capability} }

// PermissionDenied reports a missing OS permission.
func PermissionDenied(p Permission) error {
	return &Error{kind: kindPermissionDenied, Detail: string(p)}
}

// InvalidChord reports a chord that cannot be parsed or registered.
func InvalidChord(chord string) error { return &Error{kind: kindInvalidChord, Detail: chord} }

// HotkeyConflict reports a chord another application already holds.
func HotkeyConflict(chord string) error { return &Error{kind: kindHotkeyConflict, Detail: chord} }

// Timeout reports an operation that gave up after d.
func Timeout(d time.Duration) error { return &Error{kind: kindTimeout, Timeout: d} }

// Backend reports any other backend failure, formatted by [fmt.Errorf] (so %w wraps a cause
// that errors.Is and errors.As then find).
func Backend(format string, args ...any) error {
	return &Error{kind: kindBackend, Err: fmt.Errorf(format, args...)}
}
