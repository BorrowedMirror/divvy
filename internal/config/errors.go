package config

import (
	"errors"
	"fmt"
)

// IOError is a filesystem failure reading or writing the settings at Path.
type IOError struct {
	Path string
	Err  error
}

func (e *IOError) Error() string { return fmt.Sprintf("io error at %s: %v", e.Path, e.Err) }
func (e *IOError) Unwrap() error { return e.Err }

// ParseError is a settings document that is not valid TOML or does not fit the schema (a
// wrong type, an unknown enum value, a missing required field).
type ParseError struct{ Err error }

func (e *ParseError) Error() string { return fmt.Sprintf("invalid settings toml: %v", e.Err) }
func (e *ParseError) Unwrap() error { return e.Err }

// SerialiseError is a failure to encode settings as TOML.
type SerialiseError struct{ Err error }

func (e *SerialiseError) Error() string { return fmt.Sprintf("cannot serialise settings: %v", e.Err) }
func (e *SerialiseError) Unwrap() error { return e.Err }

// TooNewError is a settings version newer than [SettingsVersion].
type TooNewError struct{ Found uint32 }

func (e *TooNewError) Error() string {
	return fmt.Sprintf("settings version %d is newer than supported %d", e.Found, SettingsVersion)
}

// InvalidError is a setting that breaks a semantic rule ([Validate]).
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("invalid setting `%s`: %s", e.Field, e.Reason)
}

// ErrNoConfigDir means no home/config directory could be determined.
var ErrNoConfigDir = errors.New("no home/config directory could be determined")

func invalid(field, reason string) error { return &InvalidError{Field: field, Reason: reason} }

func invalidf(field, format string, args ...any) error {
	return &InvalidError{Field: field, Reason: fmt.Sprintf(format, args...)}
}

func ioErr(path string, err error) error { return &IOError{Path: path, Err: err} }
