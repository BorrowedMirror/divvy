package x11

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

const (
	kcShiftL  = 50
	kcShiftR  = 62
	kcCtrlL   = 37
	kcAltL    = 64
	kcSuperL  = 133
	kcNumLock = 77
	kcD       = 40
)

func testModMap() modifierMap {
	// keycodes_per_modifier = 2: Shift, Lock, Control, Mod1, Mod2, Mod3, Mod4, Mod5.
	return modifierMapFromReply([]uint8{kcShiftL, kcShiftR, 66, 0, kcCtrlL, 105, kcAltL, 0, kcNumLock, 0, 0, 0, kcSuperL, 0, 92, 0})
}

func keysDown(down ...uint8) keymap {
	var k keymap
	for _, c := range down {
		k[c/8] |= 1 << (c % 8)
	}
	return k
}

func TestModifierMapParsesRows(t *testing.T) {
	m := testModMap()
	assert.True(t, m.isModifier(kcShiftR) && m.isModifier(kcNumLock) && m.isModifier(92), "modifiers recognised")
	assert.False(t, m.isModifier(kcD), "D is not a modifier")
	assert.False(t, m.isModifier(0), "unused slots are dropped")
	assert.Equal(t, modifierMap{}, modifierMapFromReply(nil))
	k := keysDown(255)
	assert.True(t, isDown(&k, 255))
}

func TestHeldModifiersAndKeys(t *testing.T) {
	m := testModMap()
	k := keysDown(kcCtrlL, kcShiftR, kcD, kcNumLock)
	assert.Equal(t, []platform.KeyModifier{platform.Ctrl, platform.Shift}, m.held(&k))
	assert.Equal(t, []uint8{kcShiftR}, m.heldKeycodes(platform.Shift, &k))
	assert.True(t, m.nonModifierDown(&k), "D is down")
	onlyMods := keysDown(kcCtrlL, kcNumLock)
	assert.False(t, m.nonModifierDown(&onlyMods))
	altSuper := keysDown(kcAltL, kcSuperL)
	assert.Equal(t, []platform.KeyModifier{platform.Alt, platform.Meta}, m.held(&altSuper))

	h := heldStateFromKeymap(&m, &k)
	assert.Equal(t, []platform.KeyModifier{platform.Ctrl, platform.Shift}, h.modifiers)
	assert.Equal(t, []uint8{kcShiftR}, h.keycodesOf(platform.Shift))
	assert.Empty(t, h.keycodesOf(platform.Alt))
}

func TestWaitsUntilTheHotkeyKeyIsReleased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := testModMap()
		polls := 0
		start := time.Now()
		got, ok := waitForRelease(&m, time.Minute, func() (keymap, bool) {
			polls++
			if polls < 4 {
				return keysDown(kcCtrlL, kcD), true
			}
			return keysDown(kcCtrlL), true
		})
		assert.True(t, ok)
		assert.Equal(t, keysDown(kcCtrlL), got)
		assert.Equal(t, 4, polls)
		assert.Equal(t, 3*releasePoll, time.Since(start))
	})
}

func TestWaitIsCappedAndToleratesQueryFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := testModMap()
		polls := 0
		got, ok := waitForRelease(&m, 20*time.Millisecond, func() (keymap, bool) {
			polls++
			return keysDown(kcD), true
		})
		assert.True(t, ok, "sends anyway after the cap")
		assert.Equal(t, keysDown(kcD), got)
		assert.Equal(t, 5, polls, "20ms / 5ms poll + initial query")

		_, ok = waitForRelease(&m, 20*time.Millisecond, func() (keymap, bool) { return keymap{}, false })
		assert.False(t, ok, "query failure")
	})
}
