package mac

import (
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// copyModifierEnv selects the copy chord modifier (`cmd` or `ctrl`), read by [OptionsFromEnv].
const copyModifierEnv = "RKL_MACOS_COPY_MODIFIER"

// Options are the macOS backend tunables.
type Options struct {
	// CopyModifier is the modifier the game's copy shortcut uses (Cmd for the native client,
	// Ctrl under Wine).
	CopyModifier CopyModifier
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
	if v, ok := lookup(copyModifierEnv); ok {
		if m, ok := parseCopyModifier(v); ok {
			opts.CopyModifier = m
		} else {
			slog.Warn("ignoring invalid "+copyModifierEnv, "value", v)
		}
	}
	return opts
}

// CopyModifier is which modifier the game expects for its copy shortcut on macOS.
//
// The native macOS client follows platform convention (Cmd+C / Cmd+Option+C); the Windows
// client under CrossOver/Wine receives Ctrl+C. Configurable because neither is verified on
// every setup (see docs/research/macos.md).
type CopyModifier int

const (
	// CopyCommand sends Cmd for the chord's Ctrl (the native client); the default.
	CopyCommand CopyModifier = iota
	// CopyControl sends Ctrl unchanged (the Windows client under Wine).
	CopyControl
)

// parseCopyModifier parses `cmd`/`command`/`meta` or `ctrl`/`control` (case-insensitive).
func parseCopyModifier(s string) (CopyModifier, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "cmd", "command", "meta":
		return CopyCommand, true
	case "ctrl", "control":
		return CopyControl, true
	}
	return 0, false
}

// remapChord rewrites a portable chord (written with Ctrl, as on Windows/Linux) for the game on
// macOS.
func remapChord(chord platform.Chord, modifier CopyModifier) platform.Chord {
	mods := make([]platform.KeyModifier, 0, len(chord.Modifiers))
	for _, m := range chord.Modifiers {
		if modifier == CopyCommand && m == platform.Ctrl {
			m = platform.Meta
		}
		if !slices.Contains(mods, m) {
			mods = append(mods, m)
		}
	}
	return platform.Chord{Modifiers: mods, Key: chord.Key}
}
