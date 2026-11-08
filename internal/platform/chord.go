package platform

import (
	"slices"
	"strings"
)

// KeyModifier is a chord modifier.
type KeyModifier string

// Chord modifiers.
const (
	Ctrl  KeyModifier = "ctrl"
	Alt   KeyModifier = "alt"
	Shift KeyModifier = "shift"
	Meta  KeyModifier = "meta"
)

// Chord is a key chord such as `Ctrl+Alt+D`. Key uses W3C `KeyboardEvent.code` names
// (`KeyD`, `F5`).
type Chord struct {
	Modifiers []KeyModifier `json:"modifiers"`
	Key       string        `json:"key"`
}

// NewChord builds a chord. Its Modifiers are never nil, so the frontend always gets a list.
func NewChord(key string, modifiers ...KeyModifier) Chord {
	return Chord{Modifiers: append([]KeyModifier{}, modifiers...), Key: key}
}

// UnboundChord is a hotkey the user has explicitly cleared ("Not set"). It is stored as an
// empty key so it is saved and never silently replaced by the default.
func UnboundChord() Chord { return NewChord("") }

// IsUnbound reports whether the chord has no key (see [UnboundChord]).
func (c Chord) IsUnbound() bool {
	return strings.TrimSpace(c.Key) == ""
}

// Equal compares two chords, modifier order included.
func (c Chord) Equal(o Chord) bool {
	return c.Key == o.Key && slices.Equal(c.Modifiers, o.Modifiers)
}

// ParseChord parses `Ctrl+Alt+D` / `ctrl+shift+F5` style strings.
func ParseChord(s string) (Chord, error) {
	c := NewChord("")
	haveKey := false
	for raw := range strings.SplitSeq(s, "+") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		switch strings.ToLower(part) {
		case "ctrl", "control":
			c.Modifiers = append(c.Modifiers, Ctrl)
		case "alt", "option":
			c.Modifiers = append(c.Modifiers, Alt)
		case "shift":
			c.Modifiers = append(c.Modifiers, Shift)
		case "meta", "cmd", "super", "win":
			c.Modifiers = append(c.Modifiers, Meta)
		default:
			if haveKey {
				return Chord{}, InvalidChord(s)
			}
			haveKey = true
			lower := strings.ToLower(part)
			switch {
			case len(lower) == 1 && lower[0] >= 'a' && lower[0] <= 'z':
				c.Key = "Key" + strings.ToUpper(lower)
			case len(lower) == 1 && lower[0] >= '0' && lower[0] <= '9':
				c.Key = "Digit" + lower
			default:
				c.Key = part
			}
		}
	}
	if !haveKey {
		return Chord{}, InvalidChord(s)
	}
	return c, nil
}

// String formats the chord as `Ctrl+Alt+D`.
func (c Chord) String() string {
	var b strings.Builder
	for _, m := range c.Modifiers {
		switch m {
		case Ctrl:
			b.WriteString("Ctrl+")
		case Alt:
			b.WriteString("Alt+")
		case Shift:
			b.WriteString("Shift+")
		case Meta:
			b.WriteString("Meta+")
		}
	}
	k := c.Key
	if rest, ok := strings.CutPrefix(k, "Key"); ok {
		k = rest
	} else if rest, ok := strings.CutPrefix(k, "Digit"); ok {
		k = rest
	}
	b.WriteString(k)
	return b.String()
}

// Sending a chord while the user may still be holding the hotkey that triggered it: each
// backend reports which modifiers are physically held right now (GetAsyncKeyState,
// CGEventSourceFlagsState, X11 QueryKeymap; Wayland cannot tell and passes none) and implements
// [ChordKeys] over its injector; [RunChord] does the rest.

// ChordPlan is what to do for one chord given the modifiers the user is physically holding
// (typically the hotkey's own, e.g. Ctrl from Ctrl+D).
type ChordPlan struct {
	// ReleaseFirst are held modifiers that are not part of the chord: key-up them first or the
	// game would see e.g. Ctrl+Shift+C. They are not restored afterwards (restoring risks a stuck
	// modifier if the user let go meanwhile).
	ReleaseFirst []KeyModifier
	// Press are chord modifiers not already held: press before the key, release after (reverse
	// order).
	Press []KeyModifier
}

// PlanChord plans a chord. Chord modifiers the user already holds are left alone: synthetically
// releasing Ctrl while the user still holds it would desync the OS's logical modifier state (and
// on Windows let the hotkey key's auto-repeat reach the game unmodified).
func PlanChord(chord, held []KeyModifier) ChordPlan {
	var plan ChordPlan
	for _, m := range held {
		if !slices.Contains(chord, m) && !slices.Contains(plan.ReleaseFirst, m) {
			plan.ReleaseFirst = append(plan.ReleaseFirst, m)
		}
	}
	for _, m := range chord {
		if !slices.Contains(held, m) && !slices.Contains(plan.Press, m) {
			plan.Press = append(plan.Press, m)
		}
	}
	return plan
}

// ChordKeys are the key operations a backend provides to [RunChord].
type ChordKeys interface {
	// ReleaseHeld key-ups a modifier the user physically holds that is not part of the chord.
	ReleaseHeld(m KeyModifier) error
	Press(m KeyModifier) error
	Release(m KeyModifier) error
	// Click presses and releases the chord's (non-modifier) key.
	Click() error
}

// RunChord executes a chord with [PlanChord]. Whatever we pressed is always released again,
// even after a failure, so no modifier is left stuck; the first error wins.
func RunChord(chord, held []KeyModifier, keys ChordKeys) error {
	plan := PlanChord(chord, held)
	for _, m := range plan.ReleaseFirst {
		if err := keys.ReleaseHeld(m); err != nil {
			return err
		}
	}
	pressed := make([]KeyModifier, 0, len(plan.Press))
	var result error
	for _, m := range plan.Press {
		if err := keys.Press(m); err != nil {
			result = err
			break
		}
		pressed = append(pressed, m)
	}
	if result == nil {
		result = keys.Click()
	}
	for _, m := range slices.Backward(pressed) {
		if err := keys.Release(m); result == nil {
			result = err
		}
	}
	return result
}
