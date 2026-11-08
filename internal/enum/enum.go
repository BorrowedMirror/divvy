// Package enum decodes string enums: named string types whose valid values are a fixed set
// of constants.
package enum

import (
	"fmt"
	"slices"
)

// Parse sets *dst to text when it is one of values and fails otherwise, so an enum's
// UnmarshalText rejects unknown values wherever it is decoded from text (JSON strings
// included):
//
//	func (p *Phase) UnmarshalText(text []byte) error {
//		return enum.Parse(p, text, PhaseStart, PhaseEnd)
//	}
func Parse[T ~string](dst *T, text []byte, values ...T) error {
	v := T(text)
	if !slices.Contains(values, v) {
		return fmt.Errorf("unknown %T %q", v, text)
	}
	*dst = v
	return nil
}
