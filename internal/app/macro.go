package app

import (
	"errors"
	"strings"
	"unicode"

	"github.com/BorrowedMirror/divvy/internal/platform"
)

// macroStep is one step of a chat command or stash search: press a chord, or paste text
// (clipboard + Ctrl+V, which works with any keyboard layout).
type macroStep struct {
	paste bool // paste text rather than press chord
	chord platform.Chord
	text  string
}

// pressStep presses chord.
func pressStep(chord platform.Chord) macroStep { return macroStep{chord: chord} }

// pasteStep pastes text.
func pasteStep(text string) macroStep { return macroStep{paste: true, text: text} }

// equal compares two steps (only the field the kind uses).
func (s macroStep) equal(o macroStep) bool {
	if s.paste != o.paste {
		return false
	}
	if s.paste {
		return s.text == o.text
	}
	return s.chord.Equal(o.chord)
}

func press(key string, mods ...platform.KeyModifier) macroStep {
	return pressStep(platform.NewChord(key, mods...))
}

// Chat command placeholders and chaining.
const (
	// chatChain chains chat commands: `@last ty&&/kick @last` sends two lines.
	chatChain = "&&"
	// chatLast is the last player you whispered or who whispered you (from the game log).
	chatLast = "@last"
	// chatArea is the current area (from the game log).
	chatArea = "@area"
)

// Why a chat command cannot be sent.
var (
	errChatNoArea    = errors.New("@area needs the game log (Client.txt), and no area was found in it")
	errChatNoWhisper = errors.New("@last more than once needs the game log (Client.txt), and no whisper was found in it")
)

// chatLines are the lines a chat command sends, in order.
func chatLines(text string) []string {
	var lines []string
	for l := range strings.SplitSeq(text, chatChain) {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// chatPlaceholders reports which placeholders a command uses.
func chatPlaceholders(text string) (last, area bool) {
	return strings.Contains(text, chatLast), strings.Contains(text, chatArea)
}

// chatContext is what the placeholders stand for right now; empty when the game log is missing
// or has no whisper / area yet.
type chatContext struct {
	last string
	area string
}

// chatSteps is the macro for a chat command. Each line: Enter opens the chat box, Ctrl+A
// selects whatever is in it, the line is pasted over it and Enter sends it. Every line but the
// last is always sent; the last one only with send.
//
// Placeholders are filled from ctx. Without a known last whisper, a line with one `@last` still
// works: Ctrl+Enter makes the game open the chat with `@Name ` (its reply-to-last-whisper key),
// the text after `@last` goes at the end, and for a line that does not start with `@last`
// (`/invite @last`) Home + Delete drop the `@` and the text before it goes at the start.
func chatSteps(text string, send bool, ctx chatContext) ([]macroStep, error) {
	lines := chatLines(text)
	steps := []macroStep{}
	for i, line := range lines {
		if strings.Contains(line, chatArea) {
			if ctx.area == "" {
				return nil, errChatNoArea
			}
			line = strings.ReplaceAll(line, chatArea, ctx.area)
		}
		switch n := strings.Count(line, chatLast); {
		case ctx.last != "":
			// A line that starts with @last is a whisper (`@Name ty`); elsewhere it is a
			// command argument (`/invite Name`), without the @.
			name := ctx.last
			if rest, ok := strings.CutPrefix(line, chatLast); ok {
				line = "@" + name + strings.ReplaceAll(rest, chatLast, name)
			} else {
				line = strings.ReplaceAll(line, chatLast, name)
			}
			steps = append(steps, press("Enter"), press("KeyA", platform.Ctrl), pasteStep(line))
		case n == 0:
			steps = append(steps, press("Enter"), press("KeyA", platform.Ctrl), pasteStep(line))
		case n == 1:
			before, after, _ := strings.Cut(line, chatLast)
			after = strings.TrimLeftFunc(after, unicode.IsSpace)
			steps = append(steps, press("Enter", platform.Ctrl))
			if before != "" {
				steps = append(steps, press("Home"), press("Delete"), pasteStep(before))
			}
			if after != "" {
				steps = append(steps, press("End"), pasteStep(after))
			}
		default:
			return nil, errChatNoWhisper
		}
		if send || i+1 < len(lines) {
			steps = append(steps, press("Enter"))
		}
	}
	return steps, nil
}

// stashSteps is the macro for a stash search: Ctrl+F focuses the stash (or vendor) search box,
// Ctrl+A selects what is there, the text is pasted over it and Enter confirms.
func stashSteps(text string) []macroStep {
	return []macroStep{press("KeyF", platform.Ctrl), press("KeyA", platform.Ctrl), pasteStep(text), press("Enter")}
}

// wheelTabKey is the key for Ctrl + wheel over the game: wheel up shows the previous stash
// tab, down the next. ok is false for a zero delta.
func wheelTabKey(delta int32) (key string, ok bool) {
	switch {
	case delta > 0:
		return "ArrowLeft", true
	case delta < 0:
		return "ArrowRight", true
	}
	return "", false
}

// isCtrlKey reports whether key counts as Ctrl in the passive input hook.
func isCtrlKey(key string) bool {
	switch key {
	case "ControlLeft", "ControlRight", "Control", "Ctrl":
		return true
	}
	return false
}

// isCloseKey reports key names that close an unpinned overlay when seen by the passive hook.
func isCloseKey(key string) bool { return key == "Escape" || key == "Esc" }
