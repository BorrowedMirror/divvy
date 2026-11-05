//! Pure helpers for the Windows backend: no Win32 calls, so they are unit-testable on every OS.
//!
//! This file is compiled twice: as `backend::windows::logic` on Windows, and on any OS by
//! `crates/platform/tests/windows_logic.rs` via `#[path]`. It therefore only names crate-root items
//! (`crate::Rect`, ...) which that test re-exports at its own root.
use crate::{Capabilities, GAME_WINDOW_TITLE, KeyModifier, Rect, WindowEvent, WindowInfo};

/// Win32 extended window styles we touch (values from `WinUser.h`; asserted against the
/// `windows` crate constants in the backend so a typo cannot slip through).
pub const WS_EX_TOPMOST: u32 = 0x0000_0008;
pub const WS_EX_TRANSPARENT: u32 = 0x0000_0020;
pub const WS_EX_TOOLWINDOW: u32 = 0x0000_0080;
pub const WS_EX_LAYERED: u32 = 0x0008_0000;
pub const WS_EX_NOACTIVATE: u32 = 0x0800_0000;

/// Virtual-key codes for modifiers.
pub const VK_SHIFT: u16 = 0x10;
pub const VK_CONTROL: u16 = 0x11;
pub const VK_MENU: u16 = 0x12;
pub const VK_LWIN: u16 = 0x5B;

/// Whether a window title is the game's. Exact match (after trimming) so that e.g. a browser tab
/// called "Path of Exile 2 - Wiki" is never mistaken for the game.
pub fn title_matches(title: &str) -> bool {
    title.trim_matches(|c: char| c == '\0' || c.is_whitespace()) == GAME_WINDOW_TITLE
}

/// Convert a Win32 `RECT` (left, top, right, bottom) into our [`Rect`]. Inverted rects clamp to 0.
pub fn rect_from_ltrb(left: i32, top: i32, right: i32, bottom: i32) -> Rect {
    let span = |lo: i32, hi: i32| u32::try_from(i64::from(hi) - i64::from(lo)).unwrap_or(0);
    Rect { x: left, y: top, width: span(left, right), height: span(top, bottom) }
}

/// Extended style for the overlay after `prepare`: never activates, no taskbar button, topmost.
pub fn prepared_ex_style(current: u32) -> u32 {
    current | WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW | WS_EX_TOPMOST
}

/// Extended style after toggling click-through. `we_added_layered` says whether `WS_EX_LAYERED`
/// was added by us earlier (so we only remove what we added). Returns the new style and the new
/// `we_added_layered` flag.
pub fn click_through_ex_style(current: u32, enabled: bool, we_added_layered: bool) -> (u32, bool) {
    if enabled {
        let added = we_added_layered || current & WS_EX_LAYERED == 0;
        (current | WS_EX_LAYERED | WS_EX_TRANSPARENT, added)
    } else {
        let mut style = current & !WS_EX_TRANSPARENT;
        if we_added_layered {
            style &= !WS_EX_LAYERED;
        }
        (style, false)
    }
}

/// Map a W3C `KeyboardEvent.code` name to a Windows virtual-key code (layout-independent for the
/// letter/digit row, which is what games bind to).
pub fn vk_for_code(code: &str) -> Option<u16> {
    if let Some(rest) = code.strip_prefix("Key") {
        let mut chars = rest.chars();
        if let (Some(c), None) = (chars.next(), chars.next())
            && c.is_ascii_uppercase()
        {
            return Some(c as u16);
        }
        return None;
    }
    if let Some(rest) = code.strip_prefix("Digit") {
        let mut chars = rest.chars();
        if let (Some(c), None) = (chars.next(), chars.next())
            && c.is_ascii_digit()
        {
            return Some(c as u16);
        }
        return None;
    }
    if let Some(rest) = code.strip_prefix("Numpad")
        && let Ok(n @ 0..=9) = rest.parse::<u16>()
        && rest.len() == 1
    {
        return Some(0x60 + n);
    }
    if let Some(rest) = code.strip_prefix('F')
        && let Ok(n @ 1..=24) = rest.parse::<u16>()
        && !rest.starts_with('0')
    {
        return Some(0x70 + n - 1);
    }
    let vk = match code {
        "Backspace" => 0x08,
        "Tab" => 0x09,
        "Enter" | "NumpadEnter" => 0x0D,
        "Pause" => 0x13,
        "CapsLock" => 0x14,
        "Escape" => 0x1B,
        "Space" => 0x20,
        "PageUp" => 0x21,
        "PageDown" => 0x22,
        "End" => 0x23,
        "Home" => 0x24,
        "ArrowLeft" => 0x25,
        "ArrowUp" => 0x26,
        "ArrowRight" => 0x27,
        "ArrowDown" => 0x28,
        "PrintScreen" => 0x2C,
        "Insert" => 0x2D,
        "Delete" => 0x2E,
        "NumpadMultiply" => 0x6A,
        "NumpadAdd" => 0x6B,
        "NumpadSubtract" => 0x6D,
        "NumpadDecimal" => 0x6E,
        "NumpadDivide" => 0x6F,
        "ScrollLock" => 0x91,
        "Semicolon" => 0xBA,
        "Equal" => 0xBB,
        "Comma" => 0xBC,
        "Minus" => 0xBD,
        "Period" => 0xBE,
        "Slash" => 0xBF,
        "Backquote" => 0xC0,
        "BracketLeft" => 0xDB,
        "Backslash" => 0xDC,
        "BracketRight" => 0xDD,
        "Quote" => 0xDE,
        _ => return None,
    };
    Some(vk)
}

/// Virtual-key code used to query/press a modifier.
pub fn modifier_vk(m: KeyModifier) -> u16 {
    match m {
        KeyModifier::Ctrl => VK_CONTROL,
        KeyModifier::Alt => VK_MENU,
        KeyModifier::Shift => VK_SHIFT,
        KeyModifier::Meta => VK_LWIN,
    }
}

/// Event diffing for the window tracker. The Win32 hook thread feeds raw observations in and
/// forwards whatever events come out.
#[derive(Debug, Default)]
pub struct TrackerState {
    current: Option<WindowInfo>,
}

impl TrackerState {
    /// Native id of the tracked game window, if any.
    pub fn game_id(&self) -> Option<u64> {
        self.current.as_ref().map(|w| w.native_id)
    }

    /// A full rescan result (periodic timer, or after foreground changes while untracked).
    pub fn on_scan(&mut self, found: Option<WindowInfo>) -> Vec<WindowEvent> {
        let mut out = Vec::new();
        match (self.current.as_mut(), found) {
            (None, None) => {}
            (Some(_), None) => {
                self.current = None;
                out.push(WindowEvent::Lost);
            }
            (Some(cur), Some(new)) if cur.native_id == new.native_id => {
                if cur.rect != new.rect {
                    out.push(WindowEvent::Moved { rect: new.rect });
                }
                if cur.focused != new.focused {
                    out.push(WindowEvent::FocusChanged { focused: new.focused });
                }
                *cur = new;
            }
            (_, Some(new)) => {
                out.push(WindowEvent::Found(new.clone()));
                self.current = Some(new);
            }
        }
        out
    }

    /// The tracked window moved or resized.
    pub fn on_moved(&mut self, native_id: u64, rect: Rect) -> Option<WindowEvent> {
        let cur = self.current.as_mut().filter(|c| c.native_id == native_id)?;
        if cur.rect == rect {
            return None;
        }
        cur.rect = rect;
        Some(WindowEvent::Moved { rect })
    }

    /// The foreground window changed to `foreground_id`.
    pub fn on_foreground(&mut self, foreground_id: u64) -> Option<WindowEvent> {
        let cur = self.current.as_mut()?;
        let focused = cur.native_id == foreground_id;
        if cur.focused == focused {
            return None;
        }
        cur.focused = focused;
        Some(WindowEvent::FocusChanged { focused })
    }
}

/// What the Windows backend can do. Everything is available without extra permissions; the
/// notes cover the two real-world failure modes.
pub fn capabilities(global_hotkeys: bool) -> Capabilities {
    let mut notes = vec![
        "If Path of Exile 2 runs as administrator, kalandra must too: Windows (UIPI) blocks \
         injected Ctrl+C into elevated windows."
            .to_owned(),
        "Use windowed or borderless fullscreen; exclusive fullscreen hides the overlay.".to_owned(),
    ];
    if !global_hotkeys {
        notes.push("Global hotkeys unavailable: the hotkey message window could not be created.".to_owned());
    }
    Capabilities {
        window_tracking: true,
        overlay_always_on_top: true,
        overlay_click_through: true,
        global_hotkeys,
        passive_hook: true,
        keystroke_injection: true,
        clipboard: true,
        missing_permissions: Vec::new(),
        notes,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn info(id: u64, rect: Rect, focused: bool) -> WindowInfo {
        WindowInfo { title: GAME_WINDOW_TITLE.to_owned(), rect, focused, native_id: id }
    }

    const R1: Rect = Rect { x: 0, y: 0, width: 1920, height: 1080 };
    const R2: Rect = Rect { x: 10, y: 20, width: 800, height: 600 };

    #[test]
    fn title_matching_is_exact_after_trim() {
        assert!(title_matches("Path of Exile 2"));
        assert!(title_matches("  Path of Exile 2\0"));
        assert!(!title_matches("Path of Exile"));
        assert!(!title_matches("Path of Exile 2 - Wiki - Mozilla Firefox"));
        assert!(!title_matches("path of exile 2"));
        assert!(!title_matches(""));
    }

    #[test]
    fn rect_conversion() {
        assert_eq!(rect_from_ltrb(10, 20, 810, 620), R2);
        assert_eq!(rect_from_ltrb(-8, -8, 1928, 1088), Rect { x: -8, y: -8, width: 1936, height: 1096 });
        assert_eq!(rect_from_ltrb(5, 5, 0, 0), Rect { x: 5, y: 5, width: 0, height: 0 });
        assert_eq!(rect_from_ltrb(i32::MIN, 0, i32::MAX, 1).width, u32::MAX);
    }

    #[test]
    fn ex_styles() {
        let base = 0x100;
        let p = prepared_ex_style(base);
        assert_eq!(p, base | WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW | WS_EX_TOPMOST);
        assert_eq!(prepared_ex_style(p), p);

        let (on, added) = click_through_ex_style(p, true, false);
        assert_eq!(on, p | WS_EX_LAYERED | WS_EX_TRANSPARENT);
        assert!(added);
        let (on2, added2) = click_through_ex_style(on, true, added);
        assert_eq!((on2, added2), (on, true));
        let (off, added) = click_through_ex_style(on, false, added);
        assert_eq!((off, added), (p, false));

        // Already layered by its owner: keep WS_EX_LAYERED when turning click-through off.
        let layered = p | WS_EX_LAYERED;
        let (on, added) = click_through_ex_style(layered, true, false);
        assert!(!added);
        let (off, _) = click_through_ex_style(on, false, added);
        assert_eq!(off, layered);
    }

    #[test]
    fn virtual_keys() {
        assert_eq!(vk_for_code("KeyC"), Some(0x43));
        assert_eq!(vk_for_code("KeyA"), Some(0x41));
        assert_eq!(vk_for_code("Digit0"), Some(0x30));
        assert_eq!(vk_for_code("Digit9"), Some(0x39));
        assert_eq!(vk_for_code("Numpad7"), Some(0x67));
        assert_eq!(vk_for_code("F1"), Some(0x70));
        assert_eq!(vk_for_code("F12"), Some(0x7B));
        assert_eq!(vk_for_code("F24"), Some(0x87));
        assert_eq!(vk_for_code("Space"), Some(0x20));
        assert_eq!(vk_for_code("Quote"), Some(0xDE));
        for bad in ["Keyc", "KeyAB", "Key", "Digit", "Digit10", "F0", "F25", "F01", "Numpad10", "Nope", ""] {
            assert_eq!(vk_for_code(bad), None, "{bad}");
        }
        assert_eq!(modifier_vk(KeyModifier::Ctrl), VK_CONTROL);
        assert_eq!(modifier_vk(KeyModifier::Alt), VK_MENU);
        assert_eq!(modifier_vk(KeyModifier::Shift), VK_SHIFT);
        assert_eq!(modifier_vk(KeyModifier::Meta), VK_LWIN);
    }

    #[test]
    fn tracker_found_moved_focus_lost() {
        let mut s = TrackerState::default();
        assert_eq!(s.game_id(), None);
        assert!(s.on_scan(None).is_empty());
        assert_eq!(s.on_foreground(1), None);
        assert_eq!(s.on_moved(1, R2), None);

        assert_eq!(s.on_scan(Some(info(1, R1, false))), vec![WindowEvent::Found(info(1, R1, false))]);
        assert_eq!(s.game_id(), Some(1));
        assert!(s.on_scan(Some(info(1, R1, false))).is_empty());

        assert_eq!(s.on_foreground(1), Some(WindowEvent::FocusChanged { focused: true }));
        assert_eq!(s.on_foreground(1), None);
        assert_eq!(s.on_foreground(2), Some(WindowEvent::FocusChanged { focused: false }));

        assert_eq!(s.on_moved(2, R2), None, "other window");
        assert_eq!(s.on_moved(1, R2), Some(WindowEvent::Moved { rect: R2 }));
        assert_eq!(s.on_moved(1, R2), None);

        assert_eq!(
            s.on_scan(Some(info(1, R1, true))),
            vec![WindowEvent::Moved { rect: R1 }, WindowEvent::FocusChanged { focused: true }]
        );

        // Game restarted: new HWND.
        assert_eq!(s.on_scan(Some(info(3, R1, true))), vec![WindowEvent::Found(info(3, R1, true))]);
        assert_eq!(s.on_scan(None), vec![WindowEvent::Lost]);
        assert_eq!(s.game_id(), None);
    }

    #[test]
    fn capability_report() {
        let c = capabilities(true);
        assert!(c.window_tracking && c.overlay_always_on_top && c.overlay_click_through);
        assert!(c.global_hotkeys && c.passive_hook && c.keystroke_injection && c.clipboard);
        assert!(c.missing_permissions.is_empty());
        assert_eq!(c.notes.len(), 2);
        let c = capabilities(false);
        assert!(!c.global_hotkeys);
        assert_eq!(c.notes.len(), 3);
    }
}
