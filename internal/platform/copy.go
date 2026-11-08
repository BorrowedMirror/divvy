package platform

import (
	"log/slog"
	"strings"
	"time"
	"unicode"
)

// CopyOptions configures [CopyItemText].
type CopyOptions struct {
	// Advanced copies with Ctrl+Alt+C (mod tiers) instead of Ctrl+C.
	Advanced         bool
	Timeout          time.Duration
	PollInterval     time.Duration
	RestoreClipboard bool
}

// DefaultCopyOptions is an advanced copy with a 500 ms timeout, 15 ms polling and the clipboard
// restored afterwards.
func DefaultCopyOptions() CopyOptions {
	return CopyOptions{
		Advanced:         true,
		Timeout:          500 * time.Millisecond,
		PollInterval:     15 * time.Millisecond,
		RestoreClipboard: true,
	}
}

// itemHeader is the header every PoE2 item copy starts with. Kept local so this package never
// depends on core.
const itemHeader = "Item Class:"

// LooksLikeItem is a cheap check whether the clipboard holds item text, from a client in any
// language.
func LooksLikeItem(text string) bool {
	t := strings.TrimLeftFunc(text, unicode.IsSpace)
	if strings.HasPrefix(t, itemHeader) {
		return true
	}
	// Any client language: two `Key: value` lines (item class and rarity) and a separator.
	keyed := 0
	for line := range strings.Lines(t) {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if !strings.ContainsAny(l, ":：") {
			return false
		}
		if keyed++; keyed == 2 {
			break
		}
	}
	return keyed == 2 && strings.Contains(t, "\n--------")
}

// CopyChord is the chord PoE2 uses: `Ctrl+Alt+C` for the advanced (mod tiers) format, else
// `Ctrl+C`.
func CopyChord(advanced bool) Chord {
	if advanced {
		return NewChord("KeyC", Ctrl, Alt)
	}
	return NewChord("KeyC", Ctrl)
}

// CopyItemText sends the copy chord and returns the item text. It clears the clipboard first so
// a stale item is never returned, and restores the prior clipboard text afterwards when asked.
//
// Clipboard races:
//   - A clipboard holding non-text content (an image: Text reports no text but
//     [Clipboard.HasNonText] is true) is neither cleared nor restored; it cannot hold a stale
//     item, and we cannot write an image back. The item then stays on the clipboard.
//   - We restore only while the clipboard still holds what we left there (the item we read, or
//     nothing at all after our clear). If the user copied something else meanwhile, it is kept.
//   - An item that arrives after the timeout replaces the clipboard and is not restored: by then
//     we have already returned and cannot tell it apart from a copy the user made.
//
// Errors: [ErrTimeout] when nothing arrived, [ErrNoItemText] when the clipboard changed but to
// something that is not an item; clipboard/simulator errors from clearing or sending the chord
// are passed through (after restoring).
func CopyItemText(sim InputSimulator, clip Clipboard, opts CopyOptions) (string, error) {
	// A clipboard that cannot be read is treated as empty.
	prevText, prevOK, err := clip.Text()
	if err != nil {
		prevText, prevOK = "", false
	}
	nonText := !prevOK && clip.HasNonText()
	restore := opts.RestoreClipboard && !nonText
	err = nil
	if !nonText {
		err = clip.Clear()
	}
	var item string
	if err == nil {
		err = sim.SendChord(CopyChord(opts.Advanced))
	}
	if err == nil {
		item, err = poll(clip, opts)
	}
	if restore {
		restoreIfOurs(clip, prevText, prevOK, item, err == nil)
	}
	if err != nil {
		return "", err
	}
	return item, nil
}

// restoreIfOurs puts the previous clipboard text back (or clears it, when hadPrevious is false)
// unless the clipboard moved on since we touched it: it must still hold the item we read (when
// gotItem) or nothing.
func restoreIfOurs(clip Clipboard, previous string, hadPrevious bool, item string, gotItem bool) {
	now, nowOK, err := clip.Text()
	if err != nil {
		slog.Warn("clipboard unreadable; not restoring", "error", err)
		return
	}
	ours := gotItem && nowOK && now == item
	if !nowOK {
		ours = !clip.HasNonText()
	}
	if !ours {
		slog.Debug("clipboard changed by someone else during copy; not restoring")
		return
	}
	if nowOK == hadPrevious && (!nowOK || now == previous) {
		return
	}
	var restored error
	if hadPrevious {
		restored = clip.SetText(previous)
	} else {
		restored = clip.Clear()
	}
	if restored != nil {
		slog.Warn("failed to restore clipboard", "error", restored)
	}
}

// poll reads the clipboard every opts.PollInterval until it holds an item or opts.Timeout
// passed.
func poll(clip Clipboard, opts CopyOptions) (string, error) {
	step := max(opts.PollInterval, time.Millisecond)
	var waited time.Duration
	sawOtherText := false
	for {
		// Transient read errors while the game is still writing are not fatal.
		if text, ok, err := clip.Text(); err == nil && ok {
			if LooksLikeItem(text) {
				return text, nil
			}
			sawOtherText = sawOtherText || text != ""
		}
		if waited >= opts.Timeout {
			if sawOtherText {
				return "", ErrNoItemText
			}
			return "", Timeout(opts.Timeout)
		}
		time.Sleep(step)
		waited += step
	}
}
