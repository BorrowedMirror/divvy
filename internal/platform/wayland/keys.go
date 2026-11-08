package wayland

import (
	"strconv"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// letterCodes are the evdev keycodes of KeyA to KeyZ.
var letterCodes = [26]uint16{30, 48, 46, 32, 18, 33, 34, 35, 23, 36, 37, 38, 50, 49, 24, 25, 16, 19, 31, 20, 22, 47, 17, 45, 21, 44}

// namedEvdev are the evdev keycodes of the named keys a chord may use.
var namedEvdev = map[string]uint16{
	"Escape":     1,
	"Tab":        15,
	"Enter":      28,
	"Backspace":  14,
	"Delete":     111,
	"Home":       102,
	"End":        107,
	"PageUp":     104,
	"PageDown":   109,
	"ArrowUp":    103,
	"ArrowLeft":  105,
	"ArrowRight": 106,
	"ArrowDown":  108,
	"Space":      57,
}

// evdevCode is the Linux evdev keycode (input-event-codes.h) for a chord key (a W3C
// KeyboardEvent.code name). Both the virtual keyboard and ydotool send these.
func evdevCode(key string) (uint16, bool) {
	if c, ok := strings.CutPrefix(key, "Key"); ok && len(c) == 1 {
		if c[0] < 'A' || c[0] > 'Z' {
			return 0, false
		}
		return letterCodes[c[0]-'A'], true
	}
	if d, ok := strings.CutPrefix(key, "Digit"); ok {
		if n, err := strconv.ParseUint(d, 10, 16); err == nil && n <= 9 {
			if n == 0 {
				return 11, true
			}
			return uint16(n) + 1, true
		}
	}
	if f, ok := strings.CutPrefix(key, "F"); ok {
		if n, err := strconv.ParseUint(f, 10, 16); err == nil {
			switch {
			case n >= 1 && n <= 10:
				return 58 + uint16(n), true
			case n == 11:
				return 87, true
			case n == 12:
				return 88, true
			}
			return 0, false
		}
	}
	code, ok := namedEvdev[key]
	return code, ok
}

// modifierCode is the evdev keycode of a modifier's left key.
func modifierCode(m platform.KeyModifier) uint16 {
	switch m {
	case platform.Ctrl:
		return 29
	case platform.Shift:
		return 42
	case platform.Alt:
		return 56
	default: // Meta
		return 125
	}
}

// keyStep is one key event: an evdev code going down or up.
type keyStep struct {
	code uint16
	down bool
}

// chordSteps is the key sequence for a chord with [platform.RunChord] semantics. Wayland cannot
// tell which modifiers are physically held, so none are: every chord modifier is pressed before
// the key and released after it, in reverse order.
func chordSteps(chord platform.Chord) ([]keyStep, error) {
	key, ok := evdevCode(chord.Key)
	if !ok {
		return nil, platform.InvalidChord(chord.String())
	}
	rec := &stepRecorder{key: key}
	if err := platform.RunChord(chord.Modifiers, nil, rec); err != nil {
		return nil, err
	}
	return rec.steps, nil
}

type stepRecorder struct {
	key   uint16
	steps []keyStep
}

func (r *stepRecorder) ReleaseHeld(m platform.KeyModifier) error { return r.Release(m) }

func (r *stepRecorder) Press(m platform.KeyModifier) error {
	r.steps = append(r.steps, keyStep{modifierCode(m), true})
	return nil
}

func (r *stepRecorder) Release(m platform.KeyModifier) error {
	r.steps = append(r.steps, keyStep{modifierCode(m), false})
	return nil
}

func (r *stepRecorder) Click() error {
	r.steps = append(r.steps, keyStep{r.key, true}, keyStep{r.key, false})
	return nil
}

// keyArgs are the `ydotool key` arguments for a chord: modifiers down, key down/up, modifiers
// up in reverse order.
func keyArgs(chord platform.Chord) ([]string, error) {
	steps, err := chordSteps(chord)
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, len(steps)+1)
	args = append(args, "key")
	for _, s := range steps {
		state := 0
		if s.down {
			state = 1
		}
		args = append(args, strconv.Itoa(int(s.code))+":"+strconv.Itoa(state))
	}
	return args, nil
}
