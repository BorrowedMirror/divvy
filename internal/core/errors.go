package core

import (
	"errors"
	"fmt"
)

// Errors returned by [ParseItem].
var (
	// ErrEmpty: the input was empty or whitespace only.
	ErrEmpty = errors.New("item text is empty")
	// ErrNotAnItem: the input did not start with an `Item Class:` header (not a PoE2 item
	// copy).
	ErrNotAnItem = errors.New("not a Path of Exile 2 item: missing `Item Class:` header")
)

// UnknownRarityError is returned by [ParseItem] when the rarity line holds an unknown value.
type UnknownRarityError struct {
	Value string
}

func (e *UnknownRarityError) Error() string { return fmt.Sprintf("unknown rarity `%s`", e.Value) }

// MalformedError is returned by [ParseItem] for a structural problem with a named section.
type MalformedError struct {
	Section string
	Reason  string
}

func (e *MalformedError) Error() string {
	return fmt.Sprintf("malformed section `%s`: %s", e.Section, e.Reason)
}
