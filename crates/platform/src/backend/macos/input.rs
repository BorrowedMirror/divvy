//! Passive hook (rdev) and keystroke injection (enigo).
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, sync_channel};
use std::time::Duration;

use enigo::{Direction, Enigo, Key, Keyboard, Settings};

use super::keymap;
use super::model::{self, CopyModifier, PERM_ACCESSIBILITY, PERM_INPUT_MONITORING};
use super::permissions;
use crate::{Chord, InputEvent, InputListener, InputSimulator, KeyModifier, PlatformError};

const CHANNEL_CAPACITY: usize = 1024;
/// Gap between synthetic key events so the game samples modifier state reliably.
const KEY_GAP: Duration = Duration::from_millis(8);

/// rdev can install its tap once per process.
static HOOK_STARTED: AtomicBool = AtomicBool::new(false);

/// rdev listen-only event tap. Until F's shared `backend/hook.rs` lands this is the single hook
/// for the macOS process; swap `start` to delegate to it once available.
#[derive(Debug)]
pub(super) struct MacInputListener;

fn map_event(event: &rdev::Event) -> Option<InputEvent> {
    // On macOS rdev's `platform_code` is the virtual keycode (kVK_*).
    let key = || keymap::code_name(event.platform_code as u16).to_owned();
    match event.event_type {
        rdev::EventType::KeyPress(_) => Some(InputEvent::KeyDown { key: key() }),
        rdev::EventType::KeyRelease(_) => Some(InputEvent::KeyUp { key: key() }),
        rdev::EventType::MouseMove { x, y } => Some(InputEvent::MouseMove { x: x as i32, y: y as i32 }),
        rdev::EventType::Wheel { delta_y, .. } => Some(InputEvent::Wheel { delta: delta_y as i32 }),
        _ => None,
    }
}

impl InputListener for MacInputListener {
    fn start(&self) -> Result<Receiver<InputEvent>, PlatformError> {
        if !permissions::current().can_listen() {
            return Err(PlatformError::PermissionDenied(PERM_INPUT_MONITORING));
        }
        if HOOK_STARTED.swap(true, Ordering::SeqCst) {
            return Err(PlatformError::AlreadyStarted);
        }
        let (tx, rx) = sync_channel(CHANNEL_CAPACITY);
        // rdev schedules its tap on the *main* run loop (driven by Tauri/AppKit), so the
        // callback runs on the main thread and `listen` returns once its own (empty) run loop
        // finishes. The thread only performs that setup.
        std::thread::Builder::new()
            .name("kalandra-macos-hook".into())
            .spawn(move || {
                let result = rdev::listen(move |event| {
                    if let Some(e) = map_event(&event) {
                        let _ = tx.try_send(e);
                    }
                });
                if let Err(e) = result {
                    tracing::warn!(error = ?e, "rdev listen failed");
                    HOOK_STARTED.store(false, Ordering::SeqCst);
                }
            })
            .map_err(|e| PlatformError::Backend(format!("spawn hook thread: {e}")))?;
        Ok(rx)
    }
}

/// Synthesises chords with enigo (CGEventPost; needs Accessibility).
#[derive(Debug)]
pub(super) struct MacInputSimulator {
    copy_modifier: CopyModifier,
}

impl MacInputSimulator {
    pub(super) fn new(copy_modifier: CopyModifier) -> Self {
        Self { copy_modifier }
    }
}

fn enigo_modifier(m: KeyModifier) -> Key {
    match m {
        KeyModifier::Ctrl => Key::Control,
        KeyModifier::Alt => Key::Option,
        KeyModifier::Shift => Key::Shift,
        KeyModifier::Meta => Key::Meta,
    }
}

impl InputSimulator for MacInputSimulator {
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError> {
        if !permissions::accessibility() {
            return Err(PlatformError::PermissionDenied(PERM_ACCESSIBILITY));
        }
        let chord = model::remap_chord(chord, self.copy_modifier);
        let keycode = keymap::mac_keycode(&chord.key).ok_or_else(|| PlatformError::InvalidChord(chord.to_string()))?;
        let settings = Settings { open_prompt_to_get_permissions: false, ..Settings::default() };
        let mut enigo = Enigo::new(&settings).map_err(|e| PlatformError::Backend(e.to_string()))?;
        let backend = |e: enigo::InputError| PlatformError::Backend(e.to_string());

        let mut pressed = Vec::with_capacity(chord.modifiers.len());
        let mut result = Ok(());
        for &m in &chord.modifiers {
            if let Err(e) = enigo.key(enigo_modifier(m), Direction::Press) {
                result = Err(backend(e));
                break;
            }
            pressed.push(m);
            std::thread::sleep(KEY_GAP);
        }
        if result.is_ok() {
            result = enigo.raw(keycode, Direction::Click).map_err(backend);
            std::thread::sleep(KEY_GAP);
        }
        // Always release what we pressed, even after an error, so no modifier sticks.
        for &m in pressed.iter().rev() {
            if let Err(e) = enigo.key(enigo_modifier(m), Direction::Release) {
                tracing::warn!(error = %e, ?m, "failed to release modifier");
            }
        }
        result
    }
}
