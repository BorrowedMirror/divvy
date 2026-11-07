package x11

import (
	"reflect"
	"slices"
	"testing"
	"time"

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

func keysDown(down ...uint8) Keymap {
	var k Keymap
	for _, c := range down {
		k[c/8] |= 1 << (c % 8)
	}
	return k
}

func TestModifierMapParsesRows(t *testing.T) {
	m := testModMap()
	if !m.isModifier(kcShiftR) || !m.isModifier(kcNumLock) || !m.isModifier(92) {
		t.Fatal("modifiers not recognised")
	}
	if m.isModifier(kcD) {
		t.Fatal("D is not a modifier")
	}
	if m.isModifier(0) {
		t.Fatal("unused slots are dropped")
	}
	if !reflect.DeepEqual(modifierMapFromReply(nil), modifierMap{}) {
		t.Fatal("empty reply")
	}
	k := keysDown(255)
	if !isDown(&k, 255) {
		t.Fatal("keycode 255")
	}
}

func TestHeldModifiersAndKeys(t *testing.T) {
	m := testModMap()
	k := keysDown(kcCtrlL, kcShiftR, kcD, kcNumLock)
	if got := m.held(&k); !slices.Equal(got, []platform.KeyModifier{platform.Ctrl, platform.Shift}) {
		t.Fatalf("held = %v", got)
	}
	if got := m.heldKeycodes(platform.Shift, &k); !slices.Equal(got, []uint8{kcShiftR}) {
		t.Fatalf("shift keycodes = %v", got)
	}
	if !m.nonModifierDown(&k) {
		t.Fatal("D is down")
	}
	if k2 := keysDown(kcCtrlL, kcNumLock); m.nonModifierDown(&k2) {
		t.Fatal("only modifiers are down")
	}
	if k3 := keysDown(kcAltL, kcSuperL); !slices.Equal(m.held(&k3), []platform.KeyModifier{platform.Alt, platform.Meta}) {
		t.Fatalf("alt+super = %v", m.held(&k3))
	}

	h := heldStateFromKeymap(&m, &k)
	if !slices.Equal(h.modifiers, []platform.KeyModifier{platform.Ctrl, platform.Shift}) {
		t.Fatalf("modifiers = %v", h.modifiers)
	}
	if !slices.Equal(h.keycodesOf(platform.Shift), []uint8{kcShiftR}) {
		t.Fatalf("shift = %v", h.keycodesOf(platform.Shift))
	}
	if len(h.keycodesOf(platform.Alt)) != 0 {
		t.Fatal("alt not held")
	}
}

func TestWaitsUntilTheHotkeyKeyIsReleased(t *testing.T) {
	m := testModMap()
	polls := 0
	got, ok := waitForRelease(&m, time.Minute, func() (Keymap, bool) {
		polls++
		if polls < 4 {
			return keysDown(kcCtrlL, kcD), true
		}
		return keysDown(kcCtrlL), true
	}, func(d time.Duration) {
		if d != releasePoll {
			t.Fatalf("slept %v", d)
		}
	})
	if !ok || got != keysDown(kcCtrlL) {
		t.Fatalf("got %v %v", got, ok)
	}
	if polls != 4 {
		t.Fatalf("polls = %d", polls)
	}
}

func TestWaitIsCappedAndToleratesQueryFailure(t *testing.T) {
	m := testModMap()
	polls := 0
	got, ok := waitForRelease(&m, 20*time.Millisecond, func() (Keymap, bool) {
		polls++
		return keysDown(kcD), true
	}, func(time.Duration) {})
	if !ok || got != keysDown(kcD) {
		t.Fatal("sends anyway after the cap")
	}
	if polls != 5 {
		t.Fatalf("20ms / 5ms poll + initial query: polls = %d", polls)
	}
	if _, ok := waitForRelease(&m, 20*time.Millisecond, func() (Keymap, bool) { return Keymap{}, false }, func(time.Duration) {}); ok {
		t.Fatal("query failure")
	}
}
