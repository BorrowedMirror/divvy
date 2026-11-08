package platform

import (
	"fmt"
	"strings"
)

// Chord -> Wails global-shortcut accelerator mapping shared by every backend that registers
// hotkeys through Wails' GlobalShortcutManager.
//
// Pure and OS-independent, so its tests run on every host. Chord keys are W3C
// KeyboardEvent.code names, the same names the input hook reports for key events.
//
// Wails v3 accelerators are `Modifier+...+Key`, split on `+` and matched case-insensitively.
// Modifiers are `Ctrl` (physical Control everywhere), `Alt` (Option on macOS), `Shift` and
// `Super` (Windows key, Command on macOS). The key is a single printable character or one of
// Wails' named keys (`Escape`, `Space`, `Up`, `Page Up`, `F1`...`F35`, ...). Wails can only bind
// keys its native tables know (letters, digits, US punctuation, a few named keys, F1-F24 on
// Windows and X11, F1-F20 on macOS), so every other code, including the numpad, is rejected.

// acceleratorKeys maps W3C codes to Wails accelerator key names. Only keys present in Wails'
// Windows and X11 native key tables are listed.
var acceleratorKeys = func() map[string]string {
	m := map[string]string{
		"Backquote":    "`",
		"Minus":        "-",
		"Equal":        "=",
		"BracketLeft":  "[",
		"BracketRight": "]",
		"Backslash":    `\`,
		"Semicolon":    ";",
		"Quote":        "'",
		"Comma":        ",",
		"Period":       ".",
		"Slash":        "/",
		"Backspace":    "Backspace",
		"Tab":          "Tab",
		"Enter":        "Enter",
		"Escape":       "Escape",
		"Space":        "Space",
		"PageUp":       "Page Up",
		"PageDown":     "Page Down",
		"End":          "End",
		"Home":         "Home",
		"ArrowLeft":    "Left",
		"ArrowUp":      "Up",
		"ArrowRight":   "Right",
		"ArrowDown":    "Down",
		"Delete":       "Delete",
		"NumLock":      "NumLock",
	}
	for c := 'A'; c <= 'Z'; c++ {
		m["Key"+string(c)] = string(c)
	}
	for c := '0'; c <= '9'; c++ {
		m["Digit"+string(c)] = string(c)
	}
	for i := 1; i <= 24; i++ {
		f := fmt.Sprintf("F%d", i)
		m[f] = f
	}
	return m
}()

// Accelerator converts a chord whose key is a W3C KeyboardEvent.code name (`KeyD`, `F5`) into
// a Wails accelerator string such as `Ctrl+Alt+D`. Modifiers come out deduplicated in the fixed
// order Ctrl, Alt, Shift, Super, so equal chords give equal strings. Modifier-only, unknown and
// unbindable keys (key names are case-sensitive, as in the W3C spec) are [ErrInvalidChord].
func Accelerator(chord Chord) (string, error) {
	key, ok := acceleratorKeys[chord.Key]
	if !ok {
		return "", InvalidChord(chord.String())
	}
	var ctrl, alt, shift, super bool
	for _, m := range chord.Modifiers {
		switch m {
		case Ctrl:
			ctrl = true
		case Alt:
			alt = true
		case Shift:
			shift = true
		case Meta:
			super = true
		default:
			return "", InvalidChord(chord.String())
		}
	}
	parts := make([]string, 0, 5)
	for _, p := range []struct {
		on   bool
		name string
	}{{ctrl, "Ctrl"}, {alt, "Alt"}, {shift, "Shift"}, {super, "Super"}} {
		if p.on {
			parts = append(parts, p.name)
		}
	}
	return strings.Join(append(parts, key), "+"), nil
}
