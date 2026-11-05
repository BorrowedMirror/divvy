//! Input pieces shared by the Linux backends: enigo keystrokes, arboard clipboard,
//! `global-hotkey` (X11 grabs) and the rdev passive hook.
use std::collections::HashMap;
use std::str::FromStr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, sync_channel};
use std::sync::{Arc, Mutex, PoisonError};

use enigo::{Direction, Enigo, Keyboard, Settings};
use global_hotkey::hotkey::HotKey;
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState};

use crate::{
    Chord, Clipboard, HotkeyAction, HotkeyEvent, HotkeyHost, InputEvent, InputListener, InputSimulator, KeyModifier,
    PlatformError,
};

/// A display name no X server / compositor socket can have. enigo connects to *every*
/// compiled-in protocol and sends each keystroke through all of them, so the protocol
/// we do not want must be pointed at a name that fails to connect.
pub(crate) const NO_DISPLAY: &str = "kalandra-disabled-display";

fn lock<T>(m: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    m.lock().unwrap_or_else(PoisonError::into_inner)
}

/// Which protocol enigo should use.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum EnigoTarget {
    /// XTEST on `$DISPLAY` (X11 / XWayland).
    X11,
    /// zwp_virtual_keyboard_v1 on `$WAYLAND_DISPLAY` (wlroots compositors).
    Wayland,
}

/// Keystroke injection through enigo.
pub(crate) struct EnigoSimulator {
    enigo: Mutex<Enigo>,
}

impl std::fmt::Debug for EnigoSimulator {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("EnigoSimulator").finish_non_exhaustive()
    }
}

impl EnigoSimulator {
    pub(crate) fn new(target: EnigoTarget) -> Result<Self, PlatformError> {
        let settings = match target {
            EnigoTarget::X11 => Settings { wayland_display: Some(NO_DISPLAY.into()), ..Settings::default() },
            EnigoTarget::Wayland => Settings { x11_display: Some(NO_DISPLAY.into()), ..Settings::default() },
        };
        let enigo = Enigo::new(&settings).map_err(|e| PlatformError::Backend(format!("enigo: {e}")))?;
        Ok(Self { enigo: Mutex::new(enigo) })
    }
}

/// Chord key (W3C code) -> enigo key.
pub(crate) fn enigo_key(code: &str) -> Option<enigo::Key> {
    use enigo::Key as K;
    if let Some(c) = code.strip_prefix("Key").filter(|c| c.len() == 1) {
        return c.chars().next().filter(char::is_ascii_alphabetic).map(|c| K::Unicode(c.to_ascii_lowercase()));
    }
    if let Some(d) = code.strip_prefix("Digit").filter(|d| d.len() == 1) {
        return d.chars().next().filter(char::is_ascii_digit).map(K::Unicode);
    }
    Some(match code {
        "Escape" => K::Escape,
        "Enter" => K::Return,
        "Space" => K::Space,
        "Tab" => K::Tab,
        "Backspace" => K::Backspace,
        "Delete" => K::Delete,
        "Home" => K::Home,
        "End" => K::End,
        "PageUp" => K::PageUp,
        "PageDown" => K::PageDown,
        "F1" => K::F1,
        "F2" => K::F2,
        "F3" => K::F3,
        "F4" => K::F4,
        "F5" => K::F5,
        "F6" => K::F6,
        "F7" => K::F7,
        "F8" => K::F8,
        "F9" => K::F9,
        "F10" => K::F10,
        "F11" => K::F11,
        "F12" => K::F12,
        _ => return None,
    })
}

pub(crate) fn enigo_modifier(m: KeyModifier) -> enigo::Key {
    match m {
        KeyModifier::Ctrl => enigo::Key::Control,
        KeyModifier::Alt => enigo::Key::Alt,
        KeyModifier::Shift => enigo::Key::Shift,
        KeyModifier::Meta => enigo::Key::Meta,
    }
}

impl InputSimulator for EnigoSimulator {
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError> {
        let key = enigo_key(&chord.key).ok_or_else(|| PlatformError::InvalidChord(chord.to_string()))?;
        let err = |e: enigo::InputError| PlatformError::Backend(format!("enigo: {e}"));
        let mut enigo = lock(&self.enigo);
        let mut pressed = Vec::new();
        let mut result = Ok(());
        for m in &chord.modifiers {
            let k = enigo_modifier(*m);
            if let Err(e) = enigo.key(k, Direction::Press) {
                result = Err(err(e));
                break;
            }
            pressed.push(k);
        }
        if result.is_ok() {
            result = enigo.key(key, Direction::Click).map_err(err);
        }
        // Always release what we pressed, even on failure, so no modifier stays stuck.
        for k in pressed.into_iter().rev() {
            let released = enigo.key(k, Direction::Release).map_err(err);
            result = result.and(released);
        }
        result
    }
}

/// System clipboard through arboard (X11 selections, or wlr-data-control on Wayland).
pub(crate) struct ArboardClipboard {
    inner: Mutex<arboard::Clipboard>,
}

impl std::fmt::Debug for ArboardClipboard {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ArboardClipboard").finish_non_exhaustive()
    }
}

impl ArboardClipboard {
    pub(crate) fn new() -> Result<Self, PlatformError> {
        let inner = arboard::Clipboard::new().map_err(clip_err)?;
        Ok(Self { inner: Mutex::new(inner) })
    }
}

fn clip_err(e: arboard::Error) -> PlatformError {
    PlatformError::Backend(format!("clipboard: {e}"))
}

impl Clipboard for ArboardClipboard {
    fn get_text(&self) -> Result<Option<String>, PlatformError> {
        match lock(&self.inner).get_text() {
            Ok(t) if t.is_empty() => Ok(None),
            Ok(t) => Ok(Some(t)),
            Err(arboard::Error::ContentNotAvailable) => Ok(None),
            Err(e) => Err(clip_err(e)),
        }
    }
    fn set_text(&self, text: &str) -> Result<(), PlatformError> {
        lock(&self.inner).set_text(text).map_err(clip_err)
    }
    fn clear(&self) -> Result<(), PlatformError> {
        lock(&self.inner).clear().map_err(clip_err)
    }
}

/// `Chord` -> `global-hotkey` string (`ctrl+alt+KeyD`).
pub(crate) fn hotkey_string(chord: &Chord) -> String {
    let mut s = String::new();
    for m in &chord.modifiers {
        s.push_str(match m {
            KeyModifier::Ctrl => "ctrl+",
            KeyModifier::Alt => "alt+",
            KeyModifier::Shift => "shift+",
            KeyModifier::Meta => "super+",
        });
    }
    s.push_str(&chord.key);
    s
}

/// Global hotkeys via `global-hotkey` (XGrabKey on `$DISPLAY`).
pub(crate) struct GlobalHotkeys {
    manager: GlobalHotKeyManager,
    registered: Mutex<HashMap<HotkeyAction, HotKey>>,
    ids: Arc<Mutex<HashMap<u32, HotkeyAction>>>,
    events_taken: AtomicBool,
}

impl std::fmt::Debug for GlobalHotkeys {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("GlobalHotkeys").field("registered", &lock(&self.registered).len()).finish_non_exhaustive()
    }
}

impl GlobalHotkeys {
    pub(crate) fn new() -> Result<Self, PlatformError> {
        let manager = GlobalHotKeyManager::new().map_err(|e| PlatformError::Backend(format!("global-hotkey: {e}")))?;
        Ok(Self {
            manager,
            registered: Mutex::new(HashMap::new()),
            ids: Arc::new(Mutex::new(HashMap::new())),
            events_taken: AtomicBool::new(false),
        })
    }
}

impl HotkeyHost for GlobalHotkeys {
    fn register(&self, id: HotkeyAction, chord: &Chord) -> Result<(), PlatformError> {
        let hotkey =
            HotKey::from_str(&hotkey_string(chord)).map_err(|_| PlatformError::InvalidChord(chord.to_string()))?;
        self.unregister(id)?;
        self.manager.register(hotkey).map_err(|e| match e {
            global_hotkey::Error::AlreadyRegistered(_) | global_hotkey::Error::FailedToRegister(_) => {
                PlatformError::HotkeyConflict(format!("{chord}: {e}"))
            }
            other => PlatformError::Backend(format!("global-hotkey: {other}")),
        })?;
        lock(&self.ids).insert(hotkey.id(), id);
        lock(&self.registered).insert(id, hotkey);
        Ok(())
    }

    fn unregister(&self, id: HotkeyAction) -> Result<(), PlatformError> {
        let Some(hotkey) = lock(&self.registered).remove(&id) else { return Ok(()) };
        lock(&self.ids).remove(&hotkey.id());
        self.manager.unregister(hotkey).map_err(|e| PlatformError::Backend(format!("global-hotkey: {e}")))
    }

    fn unregister_all(&self) -> Result<(), PlatformError> {
        let actions: Vec<_> = lock(&self.registered).keys().copied().collect();
        actions.into_iter().try_for_each(|a| self.unregister(a))
    }

    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        if self.events_taken.swap(true, Ordering::SeqCst) {
            return Err(PlatformError::AlreadyStarted);
        }
        let (tx, rx) = sync_channel(64);
        let ids = Arc::clone(&self.ids);
        std::thread::Builder::new()
            .name("kalandra-hotkeys".into())
            .spawn(move || {
                let source = GlobalHotKeyEvent::receiver();
                while let Ok(ev) = source.recv() {
                    if ev.state() != HotKeyState::Pressed {
                        continue;
                    }
                    let Some(action) = lock(&ids).get(&ev.id()).copied() else { continue };
                    if tx.send(HotkeyEvent { action }).is_err() {
                        break;
                    }
                }
            })
            .map_err(|e| PlatformError::Backend(format!("spawn hotkey thread: {e}")))?;
        Ok(rx)
    }
}

/// Passive hook through the shared rdev hook (XRecord on X11).
#[derive(Debug, Default)]
pub(crate) struct HookListener;

impl InputListener for HookListener {
    fn start(&self) -> Result<Receiver<InputEvent>, PlatformError> {
        crate::backend::hook::start_global_hook()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_chord_keys() {
        assert_eq!(enigo_key("KeyC"), Some(enigo::Key::Unicode('c')));
        assert_eq!(enigo_key("Digit4"), Some(enigo::Key::Unicode('4')));
        assert_eq!(enigo_key("F12"), Some(enigo::Key::F12));
        assert_eq!(enigo_key("Escape"), Some(enigo::Key::Escape));
        assert_eq!(enigo_key("Key1"), None);
        assert_eq!(enigo_key("DigitX"), None);
        assert_eq!(enigo_key("Pause"), None);
        assert_eq!(enigo_modifier(KeyModifier::Meta), enigo::Key::Meta);
    }

    #[test]
    fn hotkey_strings_parse_in_global_hotkey() {
        for s in ["Ctrl+Alt+D", "Shift+F5", "Meta+1", "Ctrl+Space"] {
            let chord = Chord::parse(s).unwrap();
            let hs = hotkey_string(&chord);
            assert!(HotKey::from_str(&hs).is_ok(), "{hs}");
        }
        assert_eq!(hotkey_string(&Chord::parse("ctrl+alt+shift+meta+d").unwrap()), "ctrl+alt+shift+super+KeyD");
    }
}
