package mac

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// TCCProbe must satisfy app.PermissionProbe (matched by method set; platform code does not
// import app).
var _ interface {
	Missing() []string
	OpenSettings(kind string)
} = TCCProbe{}

func TestOptionsFromEnv(t *testing.T) {
	env := func(v string, set bool) func(string) (string, bool) {
		return func(k string) (string, bool) { return v, set && k == copyModifierEnv }
	}
	assert.Equal(t, Options{CopyModifier: CopyCommand}, optionsFrom(env("", false), false))
	assert.Equal(t, Options{CopyModifier: CopyControl, HookMouse: true}, optionsFrom(env("ctrl", true), true))
	assert.Equal(t, Options{CopyModifier: CopyCommand}, optionsFrom(env("hyper", true), false))
}

func TestCopyModifierParseAndRemap(t *testing.T) {
	for in, want := range map[string]CopyModifier{
		" CMD ": CopyCommand, "meta": CopyCommand, "command": CopyCommand, "Control": CopyControl, "ctrl": CopyControl,
	} {
		got, ok := parseCopyModifier(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	_, ok := parseCopyModifier("hyper")
	assert.False(t, ok)
	var zero CopyModifier
	assert.Equal(t, CopyCommand, zero)

	copyChord := platform.NewChord("KeyC", platform.Ctrl, platform.Alt)
	assert.Equal(t, platform.NewChord("KeyC", platform.Meta, platform.Alt), remapChord(copyChord, CopyCommand))
	assert.Equal(t, copyChord, remapChord(copyChord, CopyControl))
	// Ctrl+Meta collapses to a single Meta instead of a duplicate.
	both := platform.NewChord("KeyC", platform.Meta, platform.Ctrl)
	assert.Equal(t, platform.NewChord("KeyC", platform.Meta), remapChord(both, CopyCommand))
}
