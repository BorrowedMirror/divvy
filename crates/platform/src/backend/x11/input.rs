//! Input pieces shared by the Linux backends: enigo keystrokes, arboard clipboard,
//! `global-hotkey` (X11 grabs) and the rdev passive hook.
use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, sync_channel};
use std::sync::{Arc, Mutex, PoisonError};

use enigo::{Direction, Enigo, Keyboard, Settings};
use global_hotkey::hotkey::HotKey;
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState};

use super::keystate::{HeldState, RELEASE_WAIT, X11KeyState};
use crate::backend::chord_plan::{ChordKeys, run_chord};
use crate::backend::ghk::to_hotkey;
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
///
/// On X11 a private key-state connection lets `send_chord` (1) wait until the triggering
/// hotkey's key is physically released, because global-hotkey's `XGrabKey` grab would otherwise
/// swallow the synthetic chord, and (2) leave held chord modifiers alone and release extra ones
/// (see [`crate::backend::chord_plan`]). On Wayland the key state is unknown: every chord
/// modifier is pressed and released, as before.
///
/// The enigo instance is created lazily and cached on success: on sway the virtual keyboard can
/// fail to initialise right after a restart, so every `send_chord` retries until it works. When
/// init fails and a `fallback` (ydotool) is set, the chord goes through the fallback instead.
pub(crate) struct EnigoSimulator {
    settings: Settings,
    enigo: Mutex<Option<Enigo>>,
    keys: Option<X11KeyState>,
    fallback: Option<Box<dyn InputSimulator>>,
}

impl std::fmt::Debug for EnigoSimulator {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("EnigoSimulator")
            .field("ready", &lock(&self.enigo).is_some())
            .field("fallback", &self.fallback.is_some())
            .finish_non_exhaustive()
    }
}

impl EnigoSimulator {
    /// Never fails: enigo itself is created on first use (see [`Self::ensure_ready`]).
    pub(crate) fn new(target: EnigoTarget) -> Self {
        let settings = match target {
            EnigoTarget::X11 => Settings { wayland_display: Some(NO_DISPLAY.into()), ..Settings::default() },
            EnigoTarget::Wayland => Settings { x11_display: Some(NO_DISPLAY.into()), ..Settings::default() },
        };
        let keys = match target {
            EnigoTarget::X11 => X11KeyState::connect(None)
                .inspect_err(|e| tracing::warn!(error = %e, "x11 key state unavailable; held keys ignored"))
                .ok(),
            EnigoTarget::Wayland => None,
        };
        Self { settings, enigo: Mutex::new(None), keys, fallback: None }
    }

    /// Use `fallback` whenever enigo cannot be initialised.
    pub(crate) fn with_fallback(mut self, fallback: Box<dyn InputSimulator>) -> Self {
        self.fallback = Some(fallback);
        self
    }

    /// Initialise enigo now if it is not yet (cached on success; retried on the next call after
    /// a failure).
    pub(crate) fn ensure_ready(&self) -> Result<(), PlatformError> {
        init(&self.settings, &mut lock(&self.enigo)).map(|_| ())
    }
}

fn init<'a>(settings: &Settings, slot: &'a mut Option<Enigo>) -> Result<&'a mut Enigo, PlatformError> {
    if slot.is_none() {
        *slot = Some(Enigo::new(settings).map_err(|e| PlatformError::Backend(format!("enigo: {e}")))?);
    }
    slot.as_mut().ok_or_else(|| PlatformError::Backend("enigo: not initialised".into()))
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

struct Keys<'a> {
    enigo: &'a mut Enigo,
    key: enigo::Key,
    held: HeldState,
}

fn enigo_err(e: enigo::InputError) -> PlatformError {
    PlatformError::Backend(format!("enigo: {e}"))
}

impl ChordKeys for Keys<'_> {
    type Error = PlatformError;
    /// Release the exact physical keycodes (e.g. Shift_R), not just the default keysym's.
    fn release_held(&mut self, m: KeyModifier) -> Result<(), PlatformError> {
        let codes = self.held.keycodes_of(m).to_vec();
        if codes.is_empty() {
            return self.release(m);
        }
        codes.into_iter().try_for_each(|c| self.enigo.raw(u16::from(c), Direction::Release).map_err(enigo_err))
    }
    fn press(&mut self, m: KeyModifier) -> Result<(), PlatformError> {
        self.enigo.key(enigo_modifier(m), Direction::Press).map_err(enigo_err)
    }
    fn release(&mut self, m: KeyModifier) -> Result<(), PlatformError> {
        self.enigo.key(enigo_modifier(m), Direction::Release).map_err(enigo_err)
    }
    fn click(&mut self) -> Result<(), PlatformError> {
        self.enigo.key(self.key, Direction::Click).map_err(enigo_err)
    }
}

impl InputSimulator for EnigoSimulator {
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError> {
        let key = enigo_key(&chord.key).ok_or_else(|| PlatformError::InvalidChord(chord.to_string()))?;
        let mut slot = lock(&self.enigo);
        let enigo = match init(&self.settings, &mut slot) {
            Ok(enigo) => enigo,
            Err(e) => {
                let Some(fallback) = &self.fallback else { return Err(e) };
                tracing::debug!(error = %e, "enigo unavailable; using fallback simulator");
                return fallback.send_chord(chord);
            }
        };
        let held = self.keys.as_ref().and_then(|k| k.settle(RELEASE_WAIT)).unwrap_or_default();
        let held_mods = held.modifiers.clone();
        run_chord(&chord.modifiers, &held_mods, &mut Keys { enigo, key, held })
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
        let hotkey = to_hotkey(chord)?;
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

    /// An enigo that can never connect (both protocols pointed at a non-existent display).
    fn unreachable_enigo() -> EnigoSimulator {
        let settings = Settings {
            x11_display: Some(NO_DISPLAY.into()),
            wayland_display: Some(NO_DISPLAY.into()),
            ..Settings::default()
        };
        EnigoSimulator { settings, enigo: Mutex::new(None), keys: None, fallback: None }
    }

    #[test]
    fn lazy_enigo_retries_and_uses_fallback() {
        let sim = unreachable_enigo();
        let chord = Chord::parse("ctrl+c").unwrap();
        assert!(sim.ensure_ready().is_err());
        assert!(matches!(sim.send_chord(&chord), Err(PlatformError::Backend(_))), "retried, still failing");
        assert!(format!("{sim:?}").contains("ready: false"));
        assert!(matches!(sim.send_chord(&Chord::new(&[], "NoSuchKey")), Err(PlatformError::InvalidChord(_))));

        let mut fallback = crate::MockInputSimulator::new();
        fallback.expect_send_chord().times(2).returning(|_| Ok(()));
        let sim = unreachable_enigo().with_fallback(Box::new(fallback));
        sim.send_chord(&chord).unwrap();
        sim.send_chord(&chord).unwrap();
    }

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
}
