//! Passive input hook (the shared `backend::hook`) and copy-chord injection (`enigo`).
use std::sync::mpsc::Receiver;

use enigo::{Direction, Enigo, Key, Keyboard, Settings};
use windows::Win32::UI::Input::KeyboardAndMouse::{GetAsyncKeyState, VK_CONTROL, VK_LWIN, VK_MENU, VK_SHIFT};

use super::logic::{self, modifier_vk, plan_chord, vk_for_code};
use crate::{Chord, InputEvent, InputListener, InputSimulator, KeyModifier, PlatformError};

// The pure key mapping uses its own constants; pin a sample of them to the `windows` crate's.
const _: () = {
    assert!(logic::VK_SHIFT == VK_SHIFT.0);
    assert!(logic::VK_CONTROL == VK_CONTROL.0);
    assert!(logic::VK_MENU == VK_MENU.0);
    assert!(logic::VK_LWIN == VK_LWIN.0);
};

#[cfg(test)]
#[test]
fn vk_mapping_matches_windows_crate() {
    use windows::Win32::UI::Input::KeyboardAndMouse::{VK_C, VK_ESCAPE, VK_F1, VK_NUMPAD0, VK_OEM_7};

    assert_eq!(vk_for_code("KeyC"), Some(VK_C.0));
    assert_eq!(vk_for_code("Escape"), Some(VK_ESCAPE.0));
    assert_eq!(vk_for_code("F1"), Some(VK_F1.0));
    assert_eq!(vk_for_code("Numpad0"), Some(VK_NUMPAD0.0));
    assert_eq!(vk_for_code("Quote"), Some(VK_OEM_7.0));
}

/// Passive hook through the process-wide `backend::hook` (rdev low-level hooks).
#[derive(Debug)]
pub(super) struct RdevInputListener;

impl InputListener for RdevInputListener {
    fn start(&self) -> Result<Receiver<InputEvent>, PlatformError> {
        crate::backend::hook::start_global_hook()
    }
}

/// Sends chords with `enigo` (SendInput).
///
/// Modifier handling: the user is usually still holding the hotkey (e.g. Ctrl+D) when this runs.
/// Chord modifiers already held are *not* pressed or released by us: releasing Ctrl would leave D
/// physically down without Ctrl, so D auto-repeat would stop matching the RegisterHotKey binding
/// and reach the game as plain D. Held modifiers that are not part of the chord (e.g. Shift from
/// Ctrl+Shift+D) are key-upped first and not restored; restoring would risk a stuck modifier if
/// the user let go meanwhile. The key itself is sent by virtual-key code (layout independent).
#[derive(Debug)]
pub(super) struct EnigoSimulator;

fn enigo_modifier(m: KeyModifier) -> Key {
    match m {
        KeyModifier::Ctrl => Key::Control,
        KeyModifier::Alt => Key::Alt,
        KeyModifier::Shift => Key::Shift,
        KeyModifier::Meta => Key::Meta,
    }
}

fn held_modifiers() -> Vec<KeyModifier> {
    [KeyModifier::Ctrl, KeyModifier::Alt, KeyModifier::Shift, KeyModifier::Meta]
        .into_iter()
        .filter(|&m| {
            // SAFETY: GetAsyncKeyState takes any virtual-key code and has no side effects.
            let state = unsafe { GetAsyncKeyState(i32::from(modifier_vk(m))) };
            state < 0 // high bit set: key is down
        })
        .collect()
}

impl InputSimulator for EnigoSimulator {
    fn send_chord(&self, chord: &Chord) -> Result<(), PlatformError> {
        let vk = vk_for_code(&chord.key).ok_or_else(|| PlatformError::InvalidChord(chord.to_string()))?;
        let plan = plan_chord(&chord.modifiers, &held_modifiers());
        let err = |e: enigo::InputError| PlatformError::Backend(format!("enigo: {e}"));
        let mut enigo = Enigo::new(&Settings::default()).map_err(|e| PlatformError::Backend(format!("enigo: {e}")))?;
        for &m in &plan.release_first {
            enigo.key(enigo_modifier(m), Direction::Release).map_err(err)?;
        }
        let mut pressed = Vec::with_capacity(plan.press.len());
        let mut result = Ok(());
        for &m in &plan.press {
            if let Err(e) = enigo.key(enigo_modifier(m), Direction::Press) {
                result = Err(err(e));
                break;
            }
            pressed.push(m);
        }
        if result.is_ok() {
            result = enigo.key(Key::Other(u32::from(vk)), Direction::Click).map_err(err);
        }
        // Always release what we pressed, even after a failure, so no modifier is left stuck.
        for &m in pressed.iter().rev() {
            let released = enigo.key(enigo_modifier(m), Direction::Release).map_err(err);
            result = result.and(released);
        }
        result
    }
}
