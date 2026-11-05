//! Pure, OS-independent logic of the macOS backend.
//!
//! Compiled into the library on macOS, and included via `#[path]` by
//! `crates/platform/tests/macos_model.rs` so it is tested on every OS. It must therefore only
//! depend on `crate::types` (shimmed by the test) and std.
use crate::types::{Capabilities, Chord, GAME_WINDOW_TITLE, KeyModifier, Rect, WindowEvent, WindowInfo};

/// One entry of `CGWindowListCopyWindowInfo`, reduced to plain values.
///
/// `title` is `None` when the process lacks Screen Recording permission (macOS 10.15+ strips
/// `kCGWindowName` for other apps' windows).
#[derive(Debug, Clone, PartialEq, Default)]
pub struct RawWindow {
    /// `kCGWindowNumber` (CGWindowID).
    pub number: u32,
    /// `kCGWindowOwnerPID`.
    pub pid: i32,
    /// `kCGWindowLayer`; 0 is the normal application window layer.
    pub layer: i64,
    /// `kCGWindowOwnerName` (process / app name, needs no permission).
    pub owner_name: Option<String>,
    /// `kCGWindowName`.
    pub title: Option<String>,
    /// `kCGWindowBounds`, in points, global top-left origin.
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

/// Windows smaller than this (points) are helper/tooltip windows, never the game.
const MIN_GAME_SIDE: f64 = 64.0;

/// App/process names the game may run under. Used only when titles are hidden (no Screen
/// Recording permission). The native client's bundle is named after the game.
const GAME_OWNER_NAMES: &[&str] = &[GAME_WINDOW_TITLE, "PathOfExile", "PathOfExile2"];

/// Convert `kCGWindowBounds` (floating point points) into a [`Rect`].
pub fn rect_from_bounds(x: f64, y: f64, width: f64, height: f64) -> Rect {
    let clamp_i32 = |v: f64| v.round().clamp(f64::from(i32::MIN), f64::from(i32::MAX)) as i32;
    let clamp_u32 = |v: f64| if v.is_nan() { 0 } else { v.round().clamp(0.0, f64::from(u32::MAX)) as u32 };
    Rect { x: clamp_i32(x), y: clamp_i32(y), width: clamp_u32(width), height: clamp_u32(height) }
}

fn is_game_window(w: &RawWindow) -> bool {
    if w.layer != 0 || w.width < MIN_GAME_SIDE || w.height < MIN_GAME_SIDE {
        return false;
    }
    match w.title.as_deref() {
        Some(title) => title == GAME_WINDOW_TITLE,
        // Title hidden: fall back to the owning process name.
        None => w.owner_name.as_deref().is_some_and(|o| GAME_OWNER_NAMES.contains(&o)),
    }
}

/// Find the game window in an on-screen window list ordered front to back.
pub fn find_game_raw(windows: &[RawWindow]) -> Option<&RawWindow> {
    windows.iter().find(|w| is_game_window(w))
}

/// Whether `pid` owns the frontmost normal-layer window, i.e. is the active app. Overlays and
/// menus live on higher layers and are ignored.
pub fn is_frontmost(windows: &[RawWindow], pid: i32) -> bool {
    windows.iter().find(|w| w.layer == 0).is_some_and(|w| w.pid == pid)
}

/// The game window as a [`WindowInfo`], with `focused` derived from the window order.
pub fn find_game(windows: &[RawWindow]) -> Option<WindowInfo> {
    let w = find_game_raw(windows)?;
    Some(WindowInfo {
        title: w.title.clone().unwrap_or_else(|| GAME_WINDOW_TITLE.to_owned()),
        rect: rect_from_bounds(w.x, w.y, w.width, w.height),
        focused: is_frontmost(windows, w.pid),
        native_id: u64::from(w.number),
    })
}

/// Events needed to move a subscriber from `prev` to `next`.
pub fn diff(prev: Option<&WindowInfo>, next: Option<&WindowInfo>) -> Vec<WindowEvent> {
    match (prev, next) {
        (None, None) => Vec::new(),
        (None, Some(n)) => vec![WindowEvent::Found(n.clone())],
        (Some(_), None) => vec![WindowEvent::Lost],
        (Some(p), Some(n)) if p.native_id != n.native_id => vec![WindowEvent::Found(n.clone())],
        (Some(p), Some(n)) => {
            let mut out = Vec::new();
            if p.rect != n.rect {
                out.push(WindowEvent::Moved { rect: n.rect });
            }
            if p.focused != n.focused {
                out.push(WindowEvent::FocusChanged { focused: n.focused });
            }
            out
        }
    }
}

/// TCC permission state of this process.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Permissions {
    /// `AXIsProcessTrusted`: AX observers and synthetic keystrokes (CGEventPost).
    pub accessibility: bool,
    /// `CGPreflightScreenCaptureAccess`: other apps' window titles.
    pub screen_recording: bool,
    /// `CGPreflightListenEventAccess`: listen-only event tap (passive hook).
    pub input_monitoring: bool,
}

pub const PERM_ACCESSIBILITY: &str = "accessibility";
pub const PERM_SCREEN_RECORDING: &str = "screen_recording";
pub const PERM_INPUT_MONITORING: &str = "input_monitoring";

impl Permissions {
    /// Names of missing permissions, in the order the first-run guide asks for them.
    pub fn missing(&self) -> Vec<String> {
        [
            (self.accessibility, PERM_ACCESSIBILITY),
            (self.screen_recording, PERM_SCREEN_RECORDING),
            (self.input_monitoring, PERM_INPUT_MONITORING),
        ]
        .into_iter()
        .filter(|(granted, _)| !granted)
        .map(|(_, name)| name.to_owned())
        .collect()
    }

    /// A listen-only event tap works with either Input Monitoring or Accessibility.
    pub fn can_listen(&self) -> bool {
        self.input_monitoring || self.accessibility
    }
}

/// What the macOS backend can do given `perms`.
pub fn capabilities(perms: Permissions) -> Capabilities {
    let mut notes = Vec::new();
    if !perms.screen_recording {
        notes.push("Screen Recording not granted: game window matched by process name instead of title".to_owned());
    }
    if !perms.accessibility {
        notes.push("Accessibility not granted: copy keystroke disabled, window tracking polls".to_owned());
    }
    if !perms.can_listen() {
        notes.push("Input Monitoring not granted: passive keyboard/mouse hook disabled".to_owned());
    }
    Capabilities {
        // CGWindowList needs no permission; AX only makes it event-driven.
        window_tracking: true,
        overlay_always_on_top: true,
        overlay_click_through: true,
        // Carbon RegisterEventHotKey needs no TCC permission.
        global_hotkeys: true,
        passive_hook: perms.can_listen(),
        keystroke_injection: perms.accessibility,
        clipboard: true,
        missing_permissions: perms.missing(),
        notes,
    }
}

/// System Settings deep link for a permission name (see `docs/research/macos.md`).
pub fn settings_url(kind: &str) -> Option<&'static str> {
    match kind {
        PERM_ACCESSIBILITY => Some("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"),
        PERM_SCREEN_RECORDING => Some("x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"),
        PERM_INPUT_MONITORING => Some("x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"),
        _ => None,
    }
}

/// Which modifier the game expects for its copy shortcut on macOS.
///
/// The native macOS client follows platform convention (Cmd+C / Cmd+Option+C); the Windows
/// client under CrossOver/Wine receives Ctrl+C. Configurable because neither is verified on
/// every setup (see `docs/research/macos.md`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum CopyModifier {
    #[default]
    Command,
    Control,
}

impl CopyModifier {
    /// Parse `cmd`/`command`/`meta` or `ctrl`/`control` (case-insensitive).
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "cmd" | "command" | "meta" => Some(Self::Command),
            "ctrl" | "control" => Some(Self::Control),
            _ => None,
        }
    }
}

/// Rewrite a portable chord (written with Ctrl, as on Windows/Linux) for the game on macOS.
pub fn remap_chord(chord: &Chord, modifier: CopyModifier) -> Chord {
    let mut modifiers: Vec<KeyModifier> = Vec::with_capacity(chord.modifiers.len());
    for &m in &chord.modifiers {
        let m = match (modifier, m) {
            (CopyModifier::Command, KeyModifier::Ctrl) => KeyModifier::Meta,
            (_, other) => other,
        };
        if !modifiers.contains(&m) {
            modifiers.push(m);
        }
    }
    Chord { modifiers, key: chord.key.clone() }
}

/// `kCGEventFlagMask*` bits (CGEventTypes.h) for the modifiers we model.
pub const FLAG_SHIFT: u64 = 0x0002_0000;
pub const FLAG_CONTROL: u64 = 0x0004_0000;
pub const FLAG_ALTERNATE: u64 = 0x0008_0000;
pub const FLAG_COMMAND: u64 = 0x0010_0000;

/// Modifiers held according to `CGEventSourceFlagsState` bits (Option = Alt, Command = Meta).
pub fn held_from_flags(flags: u64) -> Vec<KeyModifier> {
    [
        (FLAG_CONTROL, KeyModifier::Ctrl),
        (FLAG_ALTERNATE, KeyModifier::Alt),
        (FLAG_SHIFT, KeyModifier::Shift),
        (FLAG_COMMAND, KeyModifier::Meta),
    ]
    .into_iter()
    .filter(|(bit, _)| flags & bit != 0)
    .map(|(_, m)| m)
    .collect()
}

#[cfg(test)]
mod tests {
    #[test]
    fn held_modifiers_from_cg_flags() {
        use KeyModifier::{Alt, Ctrl, Meta, Shift};
        assert!(held_from_flags(0).is_empty());
        assert_eq!(held_from_flags(FLAG_CONTROL | FLAG_SHIFT | 0x100), vec![Ctrl, Shift]);
        assert_eq!(held_from_flags(FLAG_ALTERNATE | FLAG_COMMAND), vec![Alt, Meta]);
    }

    use super::*;

    fn win(number: u32, pid: i32, title: Option<&str>, owner: Option<&str>) -> RawWindow {
        RawWindow {
            number,
            pid,
            layer: 0,
            owner_name: owner.map(str::to_owned),
            title: title.map(str::to_owned),
            x: 10.4,
            y: 20.6,
            width: 1280.0,
            height: 720.0,
        }
    }

    #[test]
    fn rect_rounds_and_clamps() {
        assert_eq!(rect_from_bounds(10.4, -20.6, 99.5, 0.2), Rect { x: 10, y: -21, width: 100, height: 0 });
        assert_eq!(
            rect_from_bounds(1e12, -1e12, -5.0, f64::NAN),
            Rect { x: i32::MAX, y: i32::MIN, width: 0, height: 0 }
        );
    }

    #[test]
    fn finds_game_by_title_and_focus() {
        let windows =
            vec![win(1, 10, Some("Path of Exile 2"), Some("Path of Exile 2")), win(2, 11, Some("Finder"), None)];
        let info = find_game(&windows).expect("game");
        assert_eq!(info.native_id, 1);
        assert_eq!(info.title, "Path of Exile 2");
        assert_eq!(info.rect, Rect { x: 10, y: 21, width: 1280, height: 720 });
        assert!(info.focused);
    }

    #[test]
    fn game_behind_another_window_is_not_focused() {
        let windows = vec![win(2, 11, Some("Safari"), None), win(1, 10, Some("Path of Exile 2"), None)];
        let info = find_game(&windows).expect("game");
        assert!(!info.focused);
        assert!(!is_frontmost(&windows, 10));
        assert!(is_frontmost(&windows, 11));
    }

    #[test]
    fn overlay_layers_do_not_steal_focus() {
        let mut overlay = win(3, 99, Some("rkl"), None);
        overlay.layer = 25;
        let windows = vec![overlay, win(1, 10, Some("Path of Exile 2"), None)];
        assert!(find_game(&windows).expect("game").focused);
        assert!(!is_frontmost(&[], 10));
    }

    #[test]
    fn falls_back_to_owner_name_without_titles() {
        let windows = vec![win(5, 10, None, Some("Path of Exile 2"))];
        let info = find_game(&windows).expect("game");
        assert_eq!(info.title, GAME_WINDOW_TITLE);
        assert_eq!(info.native_id, 5);
        assert!(find_game(&[win(6, 10, None, Some("Safari"))]).is_none());
        assert!(find_game(&[win(6, 10, None, None)]).is_none());
    }

    #[test]
    fn ignores_wrong_title_layer_and_tiny_windows() {
        // A visible title that differs wins over a matching owner name.
        assert!(find_game(&[win(1, 10, Some("Path of Exile 2 Launcher"), Some("Path of Exile 2"))]).is_none());
        let mut menu = win(1, 10, Some("Path of Exile 2"), None);
        menu.layer = 24;
        assert!(find_game(&[menu]).is_none());
        let mut tiny = win(1, 10, Some("Path of Exile 2"), None);
        tiny.width = 10.0;
        assert!(find_game(&[tiny]).is_none());
        let mut short = win(1, 10, Some("Path of Exile 2"), None);
        short.height = 10.0;
        assert!(find_game_raw(&[short]).is_none());
    }

    fn info(id: u64, x: i32, focused: bool) -> WindowInfo {
        WindowInfo {
            title: GAME_WINDOW_TITLE.to_owned(),
            rect: Rect { x, y: 0, width: 800, height: 600 },
            focused,
            native_id: id,
        }
    }

    #[test]
    fn diff_covers_every_transition() {
        assert!(diff(None, None).is_empty());
        assert_eq!(diff(None, Some(&info(1, 0, true))), vec![WindowEvent::Found(info(1, 0, true))]);
        assert_eq!(diff(Some(&info(1, 0, true)), None), vec![WindowEvent::Lost]);
        assert_eq!(diff(Some(&info(1, 0, true)), Some(&info(2, 0, true))), vec![WindowEvent::Found(info(2, 0, true))]);
        assert!(diff(Some(&info(1, 0, true)), Some(&info(1, 0, true))).is_empty());
        assert_eq!(
            diff(Some(&info(1, 0, true)), Some(&info(1, 5, false))),
            vec![
                WindowEvent::Moved { rect: Rect { x: 5, y: 0, width: 800, height: 600 } },
                WindowEvent::FocusChanged { focused: false },
            ]
        );
        assert_eq!(
            diff(Some(&info(1, 0, false)), Some(&info(1, 0, true))),
            vec![WindowEvent::FocusChanged { focused: true }]
        );
    }

    #[test]
    fn permissions_map_to_capabilities() {
        let none = capabilities(Permissions::default());
        assert_eq!(none.missing_permissions, vec!["accessibility", "screen_recording", "input_monitoring"]);
        assert!(none.window_tracking && none.global_hotkeys && none.clipboard);
        assert!(!none.passive_hook && !none.keystroke_injection);
        assert_eq!(none.notes.len(), 3);

        let all = capabilities(Permissions { accessibility: true, screen_recording: true, input_monitoring: true });
        assert!(all.missing_permissions.is_empty());
        assert!(all.notes.is_empty());
        assert!(all.passive_hook && all.keystroke_injection && all.overlay_always_on_top && all.overlay_click_through);

        // Accessibility alone is enough for the listen-only tap.
        let ax = capabilities(Permissions { accessibility: true, ..Permissions::default() });
        assert!(ax.passive_hook && ax.keystroke_injection);
        assert_eq!(ax.missing_permissions, vec!["screen_recording", "input_monitoring"]);

        let im = Permissions { input_monitoring: true, ..Permissions::default() };
        assert!(im.can_listen());
        assert!(!capabilities(im).keystroke_injection);
    }

    #[test]
    fn settings_urls() {
        assert!(settings_url(PERM_ACCESSIBILITY).unwrap().ends_with("Privacy_Accessibility"));
        assert!(settings_url(PERM_SCREEN_RECORDING).unwrap().ends_with("Privacy_ScreenCapture"));
        assert!(settings_url(PERM_INPUT_MONITORING).unwrap().ends_with("Privacy_ListenEvent"));
        assert!(settings_url("camera").is_none());
        for kind in Permissions::default().missing() {
            assert!(settings_url(&kind).unwrap().starts_with("x-apple.systempreferences:"));
        }
    }

    #[test]
    fn copy_modifier_parse_and_remap() {
        assert_eq!(CopyModifier::parse(" CMD "), Some(CopyModifier::Command));
        assert_eq!(CopyModifier::parse("meta"), Some(CopyModifier::Command));
        assert_eq!(CopyModifier::parse("command"), Some(CopyModifier::Command));
        assert_eq!(CopyModifier::parse("Control"), Some(CopyModifier::Control));
        assert_eq!(CopyModifier::parse("ctrl"), Some(CopyModifier::Control));
        assert_eq!(CopyModifier::parse("hyper"), None);
        assert_eq!(CopyModifier::default(), CopyModifier::Command);

        let copy = Chord::new(&[KeyModifier::Ctrl, KeyModifier::Alt], "KeyC");
        assert_eq!(
            remap_chord(&copy, CopyModifier::Command),
            Chord::new(&[KeyModifier::Meta, KeyModifier::Alt], "KeyC")
        );
        assert_eq!(remap_chord(&copy, CopyModifier::Control), copy);
        // Ctrl+Meta collapses to a single Meta instead of a duplicate.
        let both = Chord::new(&[KeyModifier::Meta, KeyModifier::Ctrl], "KeyC");
        assert_eq!(remap_chord(&both, CopyModifier::Command), Chord::new(&[KeyModifier::Meta], "KeyC"));
    }
}
