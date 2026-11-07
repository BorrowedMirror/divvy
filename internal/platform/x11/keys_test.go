package x11

import (
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestMapsKeysToW3CCodes(t *testing.T) {
	for kc, want := range map[uint8]string{
		54:  "KeyC",
		16:  "Digit7",
		71:  "F5",
		9:   "Escape",
		37:  "ControlLeft",
		36:  "Enter",
		90:  "Numpad0",
		104: "NumpadEnter",
		198: "F20",
		127: "Pause",
		255: "Unidentified",
		130: "Hangul",
		131: "Hanja",
		64:  "AltLeft",
		108: "AltRight",
	} {
		if got := keyCode(kc); got != want {
			t.Errorf("keyCode(%d) = %q, want %q", kc, got, want)
		}
	}
}

// Named keys map back to the same keycode.
func TestNamedKeysRoundTrip(t *testing.T) {
	for _, code := range []string{"Escape", "KeyA", "Digit0", "F1", "Period", "Backquote", "Insert", "Numpad9", "IntlBackslash"} {
		kc, ok := keycodeFor(code)
		if !ok || keyCode(kc) != code {
			t.Errorf("%s -> %d -> %s", code, kc, keyCode(kc))
		}
	}
	seen := map[string]uint8{}
	for kc, name := range keycodeNames {
		if prev, dup := seen[name]; dup {
			t.Errorf("%s is both %d and %d", name, prev, kc)
		}
		seen[name] = kc
	}
}

func TestMapsArrowKeys(t *testing.T) {
	if s, ok := chordKeysym("ArrowLeft"); !ok || s != xkLeft {
		t.Fatal("ArrowLeft")
	}
	if s, ok := chordKeysym("ArrowRight"); !ok || s != xkRight {
		t.Fatal("ArrowRight")
	}
}

func TestMapsChordKeys(t *testing.T) {
	for code, want := range map[string]uint32{"KeyC": 'c', "Digit4": '4', "F12": 0xffc9, "F1": 0xffbe, "F10": 0xffc7, "Escape": xkEscape, "PageDown": xkNext} {
		if got, ok := chordKeysym(code); !ok || got != want {
			t.Errorf("chordKeysym(%q) = %#x %v, want %#x", code, got, ok, want)
		}
	}
	for _, code := range []string{"Key1", "DigitX", "Pause", "F13", "", "KeyAB"} {
		if _, ok := chordKeysym(code); ok {
			t.Errorf("chordKeysym(%q) should fail", code)
		}
	}
	if modifierKeysym(platform.Meta) != xkSuperL || modifierKeysym(platform.Ctrl) != xkControlL {
		t.Fatal("modifier keysyms")
	}
}

func TestKeysymToKeycodePrefersFirstColumn(t *testing.T) {
	// keycodes 8..10, two columns each: (a A) (b A) (c C)
	syms := []uint32{'a', 'A', 'b', 'A', 'A', 'C'}
	if kc, ok := keysymToKeycode(8, 2, syms, 'A'); !ok || kc != 10 {
		t.Fatalf("A -> %d %v (column 0 first)", kc, ok)
	}
	if kc, ok := keysymToKeycode(8, 2, syms, 'b'); !ok || kc != 9 {
		t.Fatalf("b -> %d", kc)
	}
	if _, ok := keysymToKeycode(8, 2, syms, 'z'); ok {
		t.Fatal("z")
	}
	if _, ok := keysymToKeycode(8, 0, syms, 'a'); ok {
		t.Fatal("no columns")
	}
}
