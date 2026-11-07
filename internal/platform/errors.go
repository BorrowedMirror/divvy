package platform

import (
	"errors"
	"fmt"
	"time"
)

// ErrorKind classifies a platform [Error].
type ErrorKind int

const (
	KindUnsupported ErrorKind = iota + 1
	KindPermissionDenied
	KindInvalidChord
	KindHotkeyConflict
	KindAlreadyStarted
	KindTimeout
	KindNoItemText
	KindBackend
)

// Error is a platform error. Match a kind with errors.Is against the Err* values below, or
// read Kind after errors.As.
type Error struct {
	Kind ErrorKind
	// Detail is the capability, permission, chord or backend message, depending on Kind.
	Detail  string
	Timeout time.Duration
}

func (e *Error) Error() string {
	switch e.Kind {
	case KindUnsupported:
		return fmt.Sprintf("capability `%s` is not supported on this backend", e.Detail)
	case KindPermissionDenied:
		return fmt.Sprintf("permission `%s` has not been granted", e.Detail)
	case KindInvalidChord:
		return fmt.Sprintf("invalid key chord `%s`", e.Detail)
	case KindHotkeyConflict:
		return "hotkey already registered by another application: " + e.Detail
	case KindAlreadyStarted:
		return "already started"
	case KindTimeout:
		return fmt.Sprintf("timed out after %s", e.Timeout)
	case KindNoItemText:
		return "clipboard did not contain item text"
	default:
		return "backend error: " + e.Detail
	}
}

// Is matches any error of the same kind, so errors.Is(err, platform.ErrTimeout) works for every
// timeout.
func (e *Error) Is(target error) bool {
	var t *Error
	return errors.As(target, &t) && t.Kind == e.Kind
}

// Kind sentinels for errors.Is.
var (
	ErrUnsupported      = &Error{Kind: KindUnsupported}
	ErrPermissionDenied = &Error{Kind: KindPermissionDenied}
	ErrInvalidChord     = &Error{Kind: KindInvalidChord}
	ErrHotkeyConflict   = &Error{Kind: KindHotkeyConflict}
	ErrAlreadyStarted   = &Error{Kind: KindAlreadyStarted}
	ErrTimeout          = &Error{Kind: KindTimeout}
	ErrNoItemText       = &Error{Kind: KindNoItemText}
	ErrBackend          = &Error{Kind: KindBackend}
)

// Unsupported reports a capability this backend lacks.
func Unsupported(capability string) error { return &Error{Kind: KindUnsupported, Detail: capability} }

// PermissionDenied reports a missing OS permission.
func PermissionDenied(permission string) error {
	return &Error{Kind: KindPermissionDenied, Detail: permission}
}

// InvalidChord reports a chord that cannot be parsed or registered.
func InvalidChord(chord string) error { return &Error{Kind: KindInvalidChord, Detail: chord} }

// HotkeyConflict reports a chord another application already holds.
func HotkeyConflict(chord string) error { return &Error{Kind: KindHotkeyConflict, Detail: chord} }

// Timeout reports an operation that gave up after d.
func Timeout(d time.Duration) error { return &Error{Kind: KindTimeout, Timeout: d} }

// Backend wraps any other backend failure.
func Backend(format string, args ...any) error {
	return &Error{Kind: KindBackend, Detail: fmt.Sprintf(format, args...)}
}
