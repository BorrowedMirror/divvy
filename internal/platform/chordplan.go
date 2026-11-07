package platform

import "slices"

// How to synthesise a chord while the user may still be holding the hotkey that triggered it.
//
// Shared by every backend (pure, compiled on all OSes). Each backend reports which modifiers are
// physically held right now (GetAsyncKeyState, CGEventSourceFlagsState, X11 QueryKeymap;
// Wayland cannot tell and passes an empty set) and implements [ChordKeys] over its injector.

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
