//! How to synthesise a chord while the user may still be holding the hotkey that triggered it.
//!
//! Shared by every backend (pure, compiled on all OSes). Each backend reports which modifiers are
//! physically held right now (`GetAsyncKeyState`, `CGEventSourceFlagsState`, X11 `QueryKeymap`;
//! Wayland cannot tell and passes an empty set) and implements [`ChordKeys`] over its injector.
use crate::KeyModifier;

/// What to do for one chord given the modifiers the user is physically holding (typically the
/// hotkey's own, e.g. Ctrl from Ctrl+D).
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct ChordPlan {
    /// Held modifiers that are *not* part of the chord: key-up them first or the game would see
    /// e.g. Ctrl+Shift+C. They are not restored afterwards (restoring risks a stuck modifier if
    /// the user let go meanwhile).
    pub release_first: Vec<KeyModifier>,
    /// Chord modifiers not already held: press before the key, release after (reverse order).
    pub press: Vec<KeyModifier>,
}

/// Plan a chord. Chord modifiers the user already holds are left alone: synthetically releasing
/// Ctrl while the user still holds it would desync the OS's logical modifier state (and on
/// Windows let the hotkey key's auto-repeat reach the game unmodified).
pub fn plan_chord(chord: &[KeyModifier], held: &[KeyModifier]) -> ChordPlan {
    let mut plan = ChordPlan::default();
    for &m in held {
        if !chord.contains(&m) && !plan.release_first.contains(&m) {
            plan.release_first.push(m);
        }
    }
    for &m in chord {
        if !held.contains(&m) && !plan.press.contains(&m) {
            plan.press.push(m);
        }
    }
    plan
}

/// The key operations a backend provides to [`run_chord`].
pub trait ChordKeys {
    type Error;
    /// Key-up a modifier the user physically holds that is not part of the chord.
    fn release_held(&mut self, m: KeyModifier) -> Result<(), Self::Error>;
    fn press(&mut self, m: KeyModifier) -> Result<(), Self::Error>;
    fn release(&mut self, m: KeyModifier) -> Result<(), Self::Error>;
    /// Press and release the chord's (non-modifier) key.
    fn click(&mut self) -> Result<(), Self::Error>;
}

/// Execute a chord with [`plan_chord`]. Whatever we pressed is always released again, even after
/// a failure, so no modifier is left stuck; the first error wins.
pub fn run_chord<K: ChordKeys>(chord: &[KeyModifier], held: &[KeyModifier], keys: &mut K) -> Result<(), K::Error> {
    let plan = plan_chord(chord, held);
    for &m in &plan.release_first {
        keys.release_held(m)?;
    }
    let mut pressed = Vec::with_capacity(plan.press.len());
    let mut result = Ok(());
    for &m in &plan.press {
        if let Err(e) = keys.press(m) {
            result = Err(e);
            break;
        }
        pressed.push(m);
    }
    if result.is_ok() {
        result = keys.click();
    }
    for &m in pressed.iter().rev() {
        let released = keys.release(m);
        result = result.and(released);
    }
    result
}

#[cfg(test)]
mod tests {
    use KeyModifier::{Alt, Ctrl, Shift};

    use super::*;

    #[test]
    fn plan_keeps_held_chord_modifiers() {
        // Hotkey Ctrl+D held, sending Ctrl+C: nothing to press or release.
        assert_eq!(plan_chord(&[Ctrl], &[Ctrl]), ChordPlan::default());
        // Sending Ctrl+Alt+C while Ctrl held: press Alt only.
        assert_eq!(plan_chord(&[Ctrl, Alt], &[Ctrl]), ChordPlan { release_first: vec![], press: vec![Alt] });
        // Hotkey Ctrl+Shift+D: Shift must go up first.
        assert_eq!(plan_chord(&[Ctrl], &[Ctrl, Shift]), ChordPlan { release_first: vec![Shift], press: vec![] });
        // Nothing held (or unknown, e.g. Wayland).
        assert_eq!(plan_chord(&[Ctrl, Alt], &[]), ChordPlan { release_first: vec![], press: vec![Ctrl, Alt] });
        // Duplicates collapse.
        assert_eq!(
            plan_chord(&[Ctrl, Ctrl], &[Shift, Shift]),
            ChordPlan { release_first: vec![Shift], press: vec![Ctrl] }
        );
    }

    #[derive(Default)]
    struct Log {
        ops: Vec<String>,
        fail_on: Option<&'static str>,
    }

    impl Log {
        fn op(&mut self, s: String) -> Result<(), String> {
            let fail = self.fail_on.is_some_and(|f| f == s);
            self.ops.push(s.clone());
            if fail { Err(s) } else { Ok(()) }
        }
    }

    impl ChordKeys for Log {
        type Error = String;
        fn release_held(&mut self, m: KeyModifier) -> Result<(), String> {
            self.op(format!("up-held {m:?}"))
        }
        fn press(&mut self, m: KeyModifier) -> Result<(), String> {
            self.op(format!("down {m:?}"))
        }
        fn release(&mut self, m: KeyModifier) -> Result<(), String> {
            self.op(format!("up {m:?}"))
        }
        fn click(&mut self) -> Result<(), String> {
            self.op("click".into())
        }
    }

    #[test]
    fn run_releases_extras_then_presses_missing_in_order() {
        let mut log = Log::default();
        run_chord(&[Ctrl, Alt], &[Ctrl, Shift], &mut log).unwrap();
        assert_eq!(log.ops, ["up-held Shift", "down Alt", "click", "up Alt"]);

        let mut log = Log::default();
        run_chord(&[Ctrl, Alt], &[], &mut log).unwrap();
        assert_eq!(log.ops, ["down Ctrl", "down Alt", "click", "up Alt", "up Ctrl"]);
    }

    #[test]
    fn run_always_releases_what_it_pressed() {
        let mut log = Log { fail_on: Some("down Alt"), ..Log::default() };
        assert_eq!(run_chord(&[Ctrl, Alt], &[], &mut log), Err("down Alt".into()));
        assert_eq!(log.ops, ["down Ctrl", "down Alt", "up Ctrl"]);

        let mut log = Log { fail_on: Some("click"), ..Log::default() };
        assert_eq!(run_chord(&[Ctrl], &[], &mut log), Err("click".into()));
        assert_eq!(log.ops, ["down Ctrl", "click", "up Ctrl"]);

        let mut log = Log { fail_on: Some("up Ctrl"), ..Log::default() };
        assert_eq!(run_chord(&[Ctrl], &[], &mut log), Err("up Ctrl".into()));

        let mut log = Log { fail_on: Some("up-held Shift"), ..Log::default() };
        assert!(run_chord(&[Ctrl], &[Shift], &mut log).is_err());
        assert_eq!(log.ops, ["up-held Shift"], "nothing pressed after a failed release");
    }
}
