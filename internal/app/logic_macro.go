package app

import (
	"strings"
	"unicode"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// HotkeyEffectKind says which [HotkeyEffect] fields are set.
type HotkeyEffectKind int

// HotkeyEffectKind values. The zero value is not an effect.
const (
	// EffectPriceCheck runs a price check (HotkeyEffect.Pinned).
	EffectPriceCheck HotkeyEffectKind = iota + 1
	EffectHideOverlay
	EffectShowSettings
	EffectHideSettings
	EffectOpenTrade
	// EffectRunChat runs chat command HotkeyEffect.Index.
	EffectRunChat
	// EffectRunStash runs stash search HotkeyEffect.Index.
	EffectRunStash
)

// HotkeyEffect is what a global hotkey press does. Every hotkey toggles: pressing it while its
// window is open closes it (like Esc), otherwise it opens it.
type HotkeyEffect struct {
	Kind   HotkeyEffectKind
	Pinned bool  // EffectPriceCheck
	Index  uint8 // EffectRunChat, EffectRunStash
}

// HotkeyEffectFor maps a hotkey press to its effect given which windows are open. An unknown
// action kind yields the zero HotkeyEffect.
func HotkeyEffectFor(action platform.HotkeyAction, overlayOpen, settingsOpen bool) HotkeyEffect {
	switch action.Kind {
	case platform.PriceCheck, platform.PriceCheckPinned:
		if overlayOpen {
			return HotkeyEffect{Kind: EffectHideOverlay}
		}
		return HotkeyEffect{Kind: EffectPriceCheck, Pinned: action.Kind == platform.PriceCheckPinned}
	case platform.OpenSettings:
		if settingsOpen {
			return HotkeyEffect{Kind: EffectHideSettings}
		}
		return HotkeyEffect{Kind: EffectShowSettings}
	case platform.OpenTrade:
		return HotkeyEffect{Kind: EffectOpenTrade}
	case platform.Chat:
		return HotkeyEffect{Kind: EffectRunChat, Index: action.Index}
	case platform.StashSearch:
		return HotkeyEffect{Kind: EffectRunStash, Index: action.Index}
	}
	return HotkeyEffect{}
}

// MacroStep is one step of a chat command or stash search: press a chord, or paste text
// (clipboard + Ctrl+V, which works with any keyboard layout).
type MacroStep struct {
	// IsPaste selects Text (paste) over Chord (press).
	IsPaste bool
	Chord   platform.Chord
	Text    string
}

// PressStep presses chord.
func PressStep(chord platform.Chord) MacroStep { return MacroStep{Chord: chord} }

// PasteStep pastes text.
func PasteStep(text string) MacroStep { return MacroStep{IsPaste: true, Text: text} }

// Equal compares two steps (only the field the kind uses).
func (s MacroStep) Equal(o MacroStep) bool {
	if s.IsPaste != o.IsPaste {
		return false
	}
	if s.IsPaste {
		return s.Text == o.Text
	}
	return s.Chord.Equal(o.Chord)
}

func press(key string, mods ...platform.KeyModifier) MacroStep {
	return PressStep(platform.NewChord(key, mods...))
}

// Chat command placeholders and chaining.
const (
	// ChatChain chains chat commands: `@last ty&&/kick @last` sends two lines.
	ChatChain = "&&"
	// ChatLast is the last player you whispered or who whispered you (from the game log).
	ChatLast = "@last"
	// ChatArea is the current area (from the game log).
	ChatArea = "@area"
)

// ChatLines are the lines a chat command sends, in order.
func ChatLines(text string) []string {
	var lines []string
	for l := range strings.SplitSeq(text, ChatChain) {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// ChatPlaceholders reports which placeholders a command uses.
func ChatPlaceholders(text string) (last, area bool) {
	return strings.Contains(text, ChatLast), strings.Contains(text, ChatArea)
}

// ChatContext is what the placeholders stand for right now; nil when the game log is missing
// or has no whisper / area yet.
type ChatContext struct {
	Last *string
	Area *string
}

// ChatError is why a chat command cannot be sent.
type ChatError int

// ChatError values.
const (
	ChatNoArea ChatError = iota + 1
	ChatNoWhisper
)

func (e ChatError) Error() string {
	switch e {
	case ChatNoArea:
		return "@area needs the game log (Client.txt), and no area was found in it"
	case ChatNoWhisper:
		return "@last more than once needs the game log (Client.txt), and no whisper was found in it"
	}
	return "chat error"
}

// ChatSteps is the macro for a chat command. Each line: Enter opens the chat box, Ctrl+A
// selects whatever is in it, the line is pasted over it and Enter sends it. Every line but the
// last is always sent; the last one only with send.
//
// Placeholders are filled from ctx. Without a known last whisper, a line with one `@last` still
// works: Ctrl+Enter makes the game open the chat with `@Name ` (its reply-to-last-whisper key),
// the text after `@last` goes at the end, and for a line that does not start with `@last`
// (`/invite @last`) Home + Delete drop the `@` and the text before it goes at the start.
func ChatSteps(text string, send bool, ctx ChatContext) ([]MacroStep, error) {
	lines := ChatLines(text)
	steps := []MacroStep{}
	for i, line := range lines {
		if strings.Contains(line, ChatArea) {
			if ctx.Area == nil {
				return nil, ChatNoArea
			}
			line = strings.ReplaceAll(line, ChatArea, *ctx.Area)
		}
		switch n := strings.Count(line, ChatLast); {
		case ctx.Last != nil:
			// A line that starts with @last is a whisper (`@Name ty`); elsewhere it is a
			// command argument (`/invite Name`), without the @.
			name := *ctx.Last
			if rest, ok := strings.CutPrefix(line, ChatLast); ok {
				line = "@" + name + strings.ReplaceAll(rest, ChatLast, name)
			} else {
				line = strings.ReplaceAll(line, ChatLast, name)
			}
			steps = append(steps, press("Enter"), press("KeyA", platform.Ctrl), PasteStep(line))
		case n == 0:
			steps = append(steps, press("Enter"), press("KeyA", platform.Ctrl), PasteStep(line))
		case n == 1:
			before, after, _ := strings.Cut(line, ChatLast)
			after = strings.TrimLeftFunc(after, unicode.IsSpace)
			steps = append(steps, press("Enter", platform.Ctrl))
			if before != "" {
				steps = append(steps, press("Home"), press("Delete"), PasteStep(before))
			}
			if after != "" {
				steps = append(steps, press("End"), PasteStep(after))
			}
		default:
			return nil, ChatNoWhisper
		}
		if send || i+1 < len(lines) {
			steps = append(steps, press("Enter"))
		}
	}
	return steps, nil
}

// StashSteps is the macro for a stash search: Ctrl+F focuses the stash (or vendor) search box,
// Ctrl+A selects what is there, the text is pasted over it and Enter confirms.
func StashSteps(text string) []MacroStep {
	return []MacroStep{press("KeyF", platform.Ctrl), press("KeyA", platform.Ctrl), PasteStep(text), press("Enter")}
}

// WheelTabKey is the key for Ctrl + wheel over the game: wheel up shows the previous stash
// tab, down the next. ok is false for a zero delta.
func WheelTabKey(delta int32) (key string, ok bool) {
	switch {
	case delta > 0:
		return "ArrowLeft", true
	case delta < 0:
		return "ArrowRight", true
	}
	return "", false
}

// IsCtrlKey reports whether key counts as Ctrl in the passive input hook.
func IsCtrlKey(key string) bool {
	switch key {
	case "ControlLeft", "ControlRight", "Control", "Ctrl":
		return true
	}
	return false
}
