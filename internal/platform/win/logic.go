// Package win is RKL's Windows backend.
//
//   - tracker: SetWinEventHook (foreground + location change) on a dedicated message-loop
//     thread, plus a 1 s rescan timer for the game window appearing or disappearing.
//   - input: the passive low-level keyboard (and optionally mouse) hook, and SendInput chord
//     injection.
//   - overlay: ex-style based no-activate / topmost / click-through styling of an HWND.
//
// Global hotkeys and the clipboard come from Wails (see [platform.Parts]).
//
// The OS-independent parts (this file, keymap.go, queue.go) carry no build tags so their tests
// run on every OS; the Win32 calls live in *_windows.go. Every unsafe use carries a
// `// SAFETY:` comment.
package win

import (
	"strings"
	"unicode"

	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// Win32 extended window styles we touch (values from WinUser.h).
const (
	wsExTopmost     uint32 = 0x0000_0008
	wsExTransparent uint32 = 0x0000_0020
	wsExToolWindow  uint32 = 0x0000_0080
	wsExLayered     uint32 = 0x0008_0000
	wsExNoActivate  uint32 = 0x0800_0000
)

// Virtual-key codes for modifiers (pinned to x/sys/windows in input_windows.go).
const (
	vkShift   uint16 = 0x10
	vkControl uint16 = 0x11
	vkMenu    uint16 = 0x12
	vkLWin    uint16 = 0x5B
)

// titleMatches reports whether a window title is the game's. Exact match (after trimming) so
// that e.g. a browser tab called "Path of Exile 2 - Wiki" is never mistaken for the game.
func titleMatches(title string) bool {
	return strings.TrimFunc(title, func(r rune) bool { return r == 0 || unicode.IsSpace(r) }) == platform.GameWindowTitle
}

// rectFromLTRB converts a Win32 RECT (left, top, right, bottom) into a [platform.Rect].
// Inverted rects clamp to 0.
func rectFromLTRB(left, top, right, bottom int32) platform.Rect {
	span := func(lo, hi int32) uint32 { return uint32(max(int64(hi)-int64(lo), 0)) }
	return platform.Rect{X: left, Y: top, Width: span(left, right), Height: span(top, bottom)}
}

// preparedExStyle is the overlay's extended style after Prepare: never activates, no taskbar
// button, topmost.
func preparedExStyle(current uint32) uint32 {
	return current | wsExNoActivate | wsExToolWindow | wsExTopmost
}

// clickThroughExStyle is the extended style after toggling click-through. weAddedLayered says
// whether WS_EX_LAYERED was added by us earlier (so we only remove what we added). It returns
// the new style and the new weAddedLayered flag.
func clickThroughExStyle(current uint32, enabled, weAddedLayered bool) (uint32, bool) {
	if enabled {
		added := weAddedLayered || current&wsExLayered == 0
		return current | wsExLayered | wsExTransparent, added
	}
	style := current &^ wsExTransparent
	if weAddedLayered {
		style &^= wsExLayered
	}
	return style, false
}

// vkForCode maps a W3C KeyboardEvent.code name to a Windows virtual-key code (layout-independent
// for the letter/digit row, which is what games bind to).
func vkForCode(code string) (uint16, bool) {
	if rest, ok := strings.CutPrefix(code, "Key"); ok {
		if len(rest) == 1 && rest[0] >= 'A' && rest[0] <= 'Z' {
			return uint16(rest[0]), true
		}
		return 0, false
	}
	if rest, ok := strings.CutPrefix(code, "Digit"); ok {
		if len(rest) == 1 && rest[0] >= '0' && rest[0] <= '9' {
			return uint16(rest[0]), true
		}
		return 0, false
	}
	if rest, ok := strings.CutPrefix(code, "Numpad"); ok && len(rest) == 1 && rest[0] >= '0' && rest[0] <= '9' {
		return 0x60 + uint16(rest[0]-'0'), true
	}
	if rest, ok := strings.CutPrefix(code, "F"); ok {
		if n, ok := smallDecimal(rest); ok && n >= 1 && n <= 24 && rest[0] != '0' {
			return 0x70 + n - 1, true
		}
	}
	vk, ok := namedVK[code]
	return vk, ok
}

// smallDecimal parses one or two ASCII digits (Rust's `parse::<u16>` on the short suffixes we
// accept; anything longer is out of range anyway).
func smallDecimal(s string) (uint16, bool) {
	if len(s) == 0 || len(s) > 2 {
		return 0, false
	}
	var n uint16
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + uint16(s[i]-'0')
	}
	return n, true
}

var namedVK = map[string]uint16{
	"Backspace":      0x08,
	"Tab":            0x09,
	"Enter":          0x0D,
	"NumpadEnter":    0x0D,
	"Pause":          0x13,
	"CapsLock":       0x14,
	"Escape":         0x1B,
	"Space":          0x20,
	"PageUp":         0x21,
	"PageDown":       0x22,
	"End":            0x23,
	"Home":           0x24,
	"ArrowLeft":      0x25,
	"ArrowUp":        0x26,
	"ArrowRight":     0x27,
	"ArrowDown":      0x28,
	"PrintScreen":    0x2C,
	"Insert":         0x2D,
	"Delete":         0x2E,
	"NumpadMultiply": 0x6A,
	"NumpadAdd":      0x6B,
	"NumpadSubtract": 0x6D,
	"NumpadDecimal":  0x6E,
	"NumpadDivide":   0x6F,
	"ScrollLock":     0x91,
	"Semicolon":      0xBA,
	"Equal":          0xBB,
	"Comma":          0xBC,
	"Minus":          0xBD,
	"Period":         0xBE,
	"Slash":          0xBF,
	"Backquote":      0xC0,
	"BracketLeft":    0xDB,
	"Backslash":      0xDC,
	"BracketRight":   0xDD,
	"Quote":          0xDE,
}

// modifierVK is the virtual-key code used to query or press a modifier.
func modifierVK(m platform.KeyModifier) uint16 {
	switch m {
	case platform.Ctrl:
		return vkControl
	case platform.Alt:
		return vkMenu
	case platform.Shift:
		return vkShift
	default:
		return vkLWin
	}
}

// isExtendedVK reports whether a virtual key needs KEYEVENTF_EXTENDEDKEY when injected: the
// navigation block (so Shift+navigation does not depend on NumLock), right Ctrl/Alt, NumLock,
// PrintScreen and numpad divide. Same list as enigo, which the Rust backend injected with.
func isExtendedVK(vk uint16) bool {
	switch vk {
	case 0xA5, 0xA3, // VK_RMENU, VK_RCONTROL
		0x26, 0x28, 0x25, 0x27, // VK_UP, VK_DOWN, VK_LEFT, VK_RIGHT
		0x2D, 0x2E, 0x24, 0x23, 0x21, 0x22, // VK_INSERT, VK_DELETE, VK_HOME, VK_END, VK_PRIOR, VK_NEXT
		0x90, 0x2C, 0x6F: // VK_NUMLOCK, VK_SNAPSHOT, VK_DIVIDE
		return true
	}
	return false
}

// trackerState diffs raw observations into window events for the tracker. The Win32 hook
// thread feeds observations in and forwards whatever events come out.
type trackerState struct {
	current *platform.WindowInfo
}

// gameID is the native id of the tracked game window, if any.
func (s *trackerState) gameID() (uint64, bool) {
	if s.current == nil {
		return 0, false
	}
	return s.current.NativeID, true
}

// onScan takes a full rescan result (periodic timer, or after a foreground change while
// untracked).
func (s *trackerState) onScan(found *platform.WindowInfo) []platform.WindowEvent {
	var out []platform.WindowEvent
	switch {
	case s.current == nil && found == nil:
	case found == nil:
		s.current = nil
		out = append(out, platform.WindowEvent{Type: platform.WindowLost})
	case s.current != nil && s.current.NativeID == found.NativeID:
		if s.current.Rect != found.Rect {
			out = append(out, platform.WindowEvent{Type: platform.WindowMoved, Rect: found.Rect})
		}
		if s.current.Focused != found.Focused {
			out = append(out, platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: found.Focused})
		}
		cur := *found
		s.current = &cur
	default:
		cur := *found
		s.current = &cur
		out = append(out, platform.WindowEvent{Type: platform.WindowFound, Info: cur})
	}
	return out
}

// onMoved handles the tracked window moving or resizing.
func (s *trackerState) onMoved(nativeID uint64, rect platform.Rect) (platform.WindowEvent, bool) {
	if s.current == nil || s.current.NativeID != nativeID || s.current.Rect == rect {
		return platform.WindowEvent{}, false
	}
	s.current.Rect = rect
	return platform.WindowEvent{Type: platform.WindowMoved, Rect: rect}, true
}

// onForeground handles the foreground window changing to foregroundID.
func (s *trackerState) onForeground(foregroundID uint64) (platform.WindowEvent, bool) {
	if s.current == nil {
		return platform.WindowEvent{}, false
	}
	focused := s.current.NativeID == foregroundID
	if s.current.Focused == focused {
		return platform.WindowEvent{}, false
	}
	s.current.Focused = focused
	return platform.WindowEvent{Type: platform.WindowFocusChanged, Focused: focused}, true
}

// capabilities is what the Windows backend can do. Everything is available without extra
// permissions; the notes cover the two real-world failure modes. GlobalHotkeys and Clipboard
// are left for the shell, which provides both from Wails.
func capabilities() platform.Capabilities {
	return platform.Capabilities{
		WindowTracking:      true,
		OverlayAlwaysOnTop:  true,
		OverlayClickThrough: true,
		PassiveHook:         true,
		KeystrokeInjection:  true,
		MissingPermissions:  []string{},
		Notes: []string{
			"If Path of Exile 2 runs as administrator, rkl must too: Windows (UIPI) blocks " +
				"injected Ctrl+C into elevated windows.",
			"Use windowed or borderless fullscreen; exclusive fullscreen hides the overlay.",
		},
	}
}
