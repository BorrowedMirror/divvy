//! Passive hook (the shared `backend::hook`) and keystroke injection (enigo).
use std::sync::mpsc::Receiver;
use std::time::Duration;

use enigo::{Direction, Enigo, Key, Keyboard, Settings};

use super::keymap;
use super::model::{self, CopyModifier, PERM_ACCESSIBILITY, PERM_INPUT_MONITORING};
use super::permissions;
use crate::backend::chord_plan::{ChordKeys, run_chord};
use crate::{Chord, InputEvent, InputListener, InputSimulator, KeyModifier, PlatformError};

/// Gap between synthetic key events so the game samples modifier state reliably.
const KEY_GAP: Duration = Duration::from_millis(8);

/// Listen-only event tap through the process-wide `backend::hook`. rdev schedules the tap on
/// the *main* run loop (driven by Tauri/AppKit), so its callback runs on the main thread.
#[derive(Debug)]
pub(super) struct MacInputListener;

impl InputListener for MacInputListener {
    fn start(&self) -> Result<Receiver<InputEvent>, PlatformError> {
        // Without Input Monitoring (or Accessibility) the tap would be created but stay silent,
        // so refuse up front and let the caller surface the missing permission.
        if !permissions::current().can_listen() {
            return Err(PlatformError::PermissionDenied(PERM_INPUT_MONITORING));
        }
        crate::backend::hook::start_global_hook()
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

/// Modifiers physically held right now (HID system state, i.e. real keyboards only).
fn held_modifiers() -> Vec<KeyModifier> {
    use objc2_core_graphics::{CGEventSource, CGEventSourceStateID};
    model::held_from_flags(CGEventSource::flags_state(CGEventSourceStateID::HIDSystemState).0)
}

struct Keys {
    enigo: Enigo,
    keycode: u16,
}

fn backend(e: enigo::InputError) -> PlatformError {
    PlatformError::Backend(e.to_string())
}

impl ChordKeys for Keys {
    type Error = PlatformError;
    fn release_held(&mut self, m: KeyModifier) -> Result<(), PlatformError> {
        self.release(m)
    }
    fn press(&mut self, m: KeyModifier) -> Result<(), PlatformError> {
        self.enigo.key(enigo_modifier(m), Direction::Press).map_err(backend)?;
        std::thread::sleep(KEY_GAP);
        Ok(())
    }
    fn release(&mut self, m: KeyModifier) -> Result<(), PlatformError> {
        self.enigo.key(enigo_modifier(m), Direction::Release).map_err(backend).inspect_err(|e| {
            tracing::warn!(error = %e, ?m, "failed to release modifier");
        })
    }
    fn click(&mut self) -> Result<(), PlatformError> {
        self.enigo.raw(self.keycode, Direction::Click).map_err(backend)?;
        std::thread::sleep(KEY_GAP);
        Ok(())
    }
}

impl InputSimulator for MacInputSimulator {
    /// Held chord modifiers (Cmd from Cmd+D) are left down; extra held ones (Shift from
    /// Cmd+Shift+D) are released first. See [`crate::backend::chord_plan`].
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError> {
        if !permissions::accessibility() {
            return Err(PlatformError::PermissionDenied(PERM_ACCESSIBILITY));
        }
        let chord = model::remap_chord(chord, self.copy_modifier);
        let keycode = keymap::mac_keycode(&chord.key).ok_or_else(|| PlatformError::InvalidChord(chord.to_string()))?;
        let settings = Settings { open_prompt_to_get_permissions: false, ..Settings::default() };
        let enigo = Enigo::new(&settings).map_err(|e| PlatformError::Backend(e.to_string()))?;
        run_chord(&chord.modifiers, &held_modifiers(), &mut Keys { enigo, keycode })
    }
}
