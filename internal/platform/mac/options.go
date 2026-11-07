package mac

import (
	"log/slog"
	"os"
)

// CopyModifierEnv selects the copy chord modifier (`cmd` or `ctrl`), read by [OptionsFromEnv].
const CopyModifierEnv = "RKL_MACOS_COPY_MODIFIER"

// Options are the macOS backend tunables.
type Options struct {
	// CopyModifier is the modifier the game's copy shortcut uses (Cmd for the native client,
	// Ctrl under Wine).
	CopyModifier CopyModifier
	// PromptForAccessibility shows the system Accessibility prompt during [New] if not yet
	// trusted.
	PromptForAccessibility bool
	// HookMouse makes the passive hook tap mouse moves and the wheel too (Ctrl + wheel stash tab
	// switching). Off, the hook is keyboard only, which keeps the tap off the mouse path.
	HookMouse bool
}

// OptionsFromEnv returns the defaults with RKL_MACOS_COPY_MODIFIER applied when set and valid.
func OptionsFromEnv(hookMouse bool) Options {
	return optionsFrom(os.LookupEnv, hookMouse)
}

func optionsFrom(lookup func(string) (string, bool), hookMouse bool) Options {
	opts := Options{HookMouse: hookMouse}
	if v, ok := lookup(CopyModifierEnv); ok {
		if m, ok := ParseCopyModifier(v); ok {
			opts.CopyModifier = m
		} else {
			slog.Warn("ignoring invalid "+CopyModifierEnv, "value", v)
		}
	}
	return opts
}
