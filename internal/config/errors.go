package config

import (
	"errors"
	"fmt"
)

// IoError is a filesystem failure at Path (Rust `ConfigError::Io`).
type IoError struct {
	Path string
	Err  error
}

func (e *IoError) Error() string { return fmt.Sprintf("io error at %s: %v", e.Path, e.Err) }
func (e *IoError) Unwrap() error { return e.Err }

// ParseError is a settings document that is not valid TOML or does not fit the schema (a
// wrong type, an unknown enum variant, a missing required field) (Rust `ConfigError::Parse`).
type ParseError struct{ Err error }

func (e *ParseError) Error() string { return fmt.Sprintf("invalid settings toml: %v", e.Err) }
func (e *ParseError) Unwrap() error { return e.Err }

// SerialiseError is a failure to encode settings as TOML (Rust `ConfigError::Serialise`).
type SerialiseError struct{ Err error }

func (e *SerialiseError) Error() string { return fmt.Sprintf("cannot serialise settings: %v", e.Err) }
func (e *SerialiseError) Unwrap() error { return e.Err }

// TooNewError is a settings version newer than [SettingsVersion] (Rust `ConfigError::TooNew`).
type TooNewError struct{ Found uint32 }

func (e *TooNewError) Error() string {
	return fmt.Sprintf("settings version %d is newer than supported %d", e.Found, SettingsVersion)
}

// InvalidError is a setting that breaks a semantic rule (Rust `ConfigError::Invalid`).
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("invalid setting `%s`: %s", e.Field, e.Reason)
}

// ErrNoConfigDir means no home/config directory could be determined (Rust
// `ConfigError::NoConfigDir`).
var ErrNoConfigDir = errors.New("no home/config directory could be determined")

func invalid(field, reason string) error { return &InvalidError{Field: field, Reason: reason} }

func invalidf(field, format string, args ...any) error {
	return &InvalidError{Field: field, Reason: fmt.Sprintf(format, args...)}
}

func ioErr(path string, err error) error { return &IoError{Path: path, Err: err} }
