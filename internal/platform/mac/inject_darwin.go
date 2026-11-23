package mac

// Keystroke injection with CGEventPost (needs Accessibility).

import (
	"log/slog"
	"slices"
	"time"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

const (
	// keyGap is the gap between synthetic key events so the game samples modifier state reliably.
	keyGap = 8 * time.Millisecond

	cgEventSourceStatePriv int32  = -1
	cgEventSourceStateHID  int32  = 1
	fieldEventSourceUser   uint32 = 42 // kCGEventSourceUserData
	// injectedEventMarker tags our events in kCGEventSourceUserData.
	injectedEventMarker int64 = 100
)

// inputSimulator synthesises chords with CGEventPost.
type inputSimulator struct {
	copyModifier CopyModifier
}

var _ platform.InputSimulator = (*inputSimulator)(nil)

// heldModifiers returns the modifiers physically held right now (HID system state, i.e. real
// keyboards only).
func heldModifiers() []platform.KeyModifier {
	return heldFromFlags(cgEventSourceFlagsState(cgEventSourceStateHID))
}

// SendChord sends chord to the focused app. Held chord modifiers (Cmd from Cmd+D) are left down;
// extra held ones (Shift from Cmd+Shift+D) are released first. See [platform.PlanChord].
func (s *inputSimulator) SendChord(chord platform.Chord) error {
	if !accessibility() {
		return platform.PermissionDenied(platform.PermissionAccessibility)
	}
	chord = remapChord(chord, s.copyModifier)
	keycode, ok := macKeycode(chord.Key)
	if !ok {
		return platform.InvalidChord(chord.String())
	}
	source := cgEventSourceCreate(cgEventSourceStatePriv)
	if source == 0 {
		return platform.Backend("failed creating event source")
	}
	defer cfRelease(source)
	held := heldModifiers()
	// Chord modifiers the user already holds stay down, so the key event carries them too.
	var down []platform.KeyModifier
	for _, m := range chord.Modifiers {
		if slices.Contains(held, m) {
			down = append(down, m)
		}
	}
	return platform.RunChord(chord.Modifiers, held, &cgKeys{source: source, keycode: keycode, down: down})
}

// cgKeys implements [platform.ChordKeys] over CGEventPost, setting on each event the modifier
// flags of the keys currently down, as a real keyboard would.
type cgKeys struct {
	source  uintptr
	keycode uint16
	down    []platform.KeyModifier
}

var _ platform.ChordKeys = (*cgKeys)(nil)

func (k *cgKeys) post(keycode uint16, keyDown bool) error {
	event := cgEventCreateKeyboardEvent(k.source, keycode, keyDown)
	if event == 0 {
		if keyDown {
			return platform.Backend("failed creating event to press the key")
		}
		return platform.Backend("failed creating event to release the key")
	}
	defer cfRelease(event)
	cgEventSetIntegerValueField(event, fieldEventSourceUser, injectedEventMarker)
	cgEventSetFlags(event, injectFlags(k.down))
	cgEventPost(cgHIDEventTap, event)
	return nil
}

func (k *cgKeys) ReleaseHeld(m platform.KeyModifier) error { return k.Release(m) }

func (k *cgKeys) Press(m platform.KeyModifier) error {
	if !slices.Contains(k.down, m) {
		k.down = append(k.down, m)
	}
	if err := k.post(modifierKeys[m].keycode, true); err != nil {
		return err
	}
	time.Sleep(keyGap)
	return nil
}

func (k *cgKeys) Release(m platform.KeyModifier) error {
	k.down = slices.DeleteFunc(k.down, func(d platform.KeyModifier) bool { return d == m })
	err := k.post(modifierKeys[m].keycode, false)
	if err != nil {
		slog.Warn("failed to release modifier", "error", err, "modifier", m)
	}
	return err
}

func (k *cgKeys) Click() error {
	if err := k.post(k.keycode, true); err != nil {
		return err
	}
	if err := k.post(k.keycode, false); err != nil {
		return err
	}
	time.Sleep(keyGap)
	return nil
}
