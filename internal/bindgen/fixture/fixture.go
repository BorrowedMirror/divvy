// Package fixture holds the types the bindgen tests generate bindings for. The generator reads
// doc comments and parameter names from source, which it does not look for in _test.go files.
package fixture

import "context"

// Inner is nested inside Outer.
type Inner struct {
	Note *string `json:"note"`
}

// Outer is a test payload.
type Outer struct {
	// Name of the thing.
	Name   string          `json:"name"`
	Inner  Inner           `json:"inner"`
	Ratio  float64         `json:"ratio"`
	Tags   map[string]Mode `json:"tags"`
	Hidden string          `json:"-"`
	Extra  string          `json:"extra,omitempty" ts:"optional"`
	List   []Inner         `json:"list"`
	Maybe  *Inner          `json:"maybe"`
	//lint:ignore U1000 unexported fields are skipped by the generator; this checks it.
	byHand map[string]float64
}

// Mode is a string enum.
type Mode string

// Service is a bound service.
type Service struct{}

// Get returns the thing.
func (Service) Get(ctx context.Context, which Outer, mode Mode) (Outer, error) {
	return which, nil
}

// Count never fails.
func (Service) Count() int { return 0 }

// Clear only fails.
func (Service) Clear() error { return nil }
