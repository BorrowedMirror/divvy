package app

import (
	"fmt"
	"slices"
)

// parseEnum sets *dst to text when it is one of values.
func parseEnum[T ~string](dst *T, text []byte, values ...T) error {
	v := T(text)
	if !slices.Contains(values, v) {
		return fmt.Errorf("unknown %T %q", v, text)
	}
	*dst = v
	return nil
}
