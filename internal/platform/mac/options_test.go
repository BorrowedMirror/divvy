package mac

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TCCProbe must satisfy app.PermissionProbe (matched by method set; platform code does not
// import app).
var _ interface {
	Missing() []string
	OpenSettings(kind string)
} = TCCProbe{}

func TestOptionsFromEnv(t *testing.T) {
	env := func(v string, set bool) func(string) (string, bool) {
		return func(k string) (string, bool) { return v, set && k == CopyModifierEnv }
	}
	assert.Equal(t, Options{CopyModifier: CopyCommand}, optionsFrom(env("", false), false))
	assert.Equal(t, Options{CopyModifier: CopyControl, HookMouse: true}, optionsFrom(env("ctrl", true), true))
	assert.Equal(t, Options{CopyModifier: CopyCommand}, optionsFrom(env("hyper", true), false))
}
